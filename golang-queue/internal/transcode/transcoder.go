package transcode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"oxygen/worker/internal/config"
	"oxygen/worker/internal/quality"
	"oxygen/worker/internal/thumbnail"
)

type Transcoder struct {
	cfg *config.Config
	log *slog.Logger
}

type ProgressCallback func(percent int)

type ThumbnailOptions struct {
	StoryboardOutputPath string
	PosterOutputPath     string
	PosterWidth          int
	PosterHeight         int
	Config               thumbnail.Config
	Plan                 thumbnail.Plan
	JPEGQuality          int
}

type MediaInfo struct {
	Duration           float64
	DisplayAspectRatio float64
}

type probeSideData struct {
	Rotation float64 `json:"rotation"`
}

func NewTranscoder(cfg *config.Config) *Transcoder {
	return &Transcoder{
		cfg: cfg,
		log: slog.With("component", "transcode"),
	}
}

func (t *Transcoder) ProbeMedia(ctx context.Context, inputURL string) (MediaInfo, error) {
	cmd := exec.CommandContext(ctx, t.cfg.FfprobeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-select_streams", "v:0",
		"-show_entries", "format=duration:stream=width,height,sample_aspect_ratio:stream_tags=rotate:stream_side_data=rotation",
		inputURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return MediaInfo{}, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Streams []struct {
			Width             int    `json:"width"`
			Height            int    `json:"height"`
			SampleAspectRatio string `json:"sample_aspect_ratio"`
			Tags              struct {
				Rotate string `json:"rotate"`
			} `json:"tags"`
			SideDataList []probeSideData `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return MediaInfo{}, fmt.Errorf("decode ffprobe output: %w", err)
	}

	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return MediaInfo{}, fmt.Errorf("ffprobe: invalid duration %q", probe.Format.Duration)
	}
	if len(probe.Streams) == 0 || probe.Streams[0].Width <= 0 || probe.Streams[0].Height <= 0 {
		return MediaInfo{}, fmt.Errorf("ffprobe: no valid video dimensions in output")
	}

	stream := probe.Streams[0]
	sampleAspectRatio := parseSampleAspectRatio(stream.SampleAspectRatio)
	displayAspectRatio := float64(stream.Width) * sampleAspectRatio / float64(stream.Height)
	rotation := rotationDegrees(stream.Tags.Rotate, stream.SideDataList)
	if isQuarterTurn(rotation) {
		displayAspectRatio = 1 / displayAspectRatio
	}
	if displayAspectRatio <= 0 || math.IsNaN(displayAspectRatio) || math.IsInf(displayAspectRatio, 0) {
		return MediaInfo{}, fmt.Errorf("ffprobe: invalid display aspect ratio")
	}

	return MediaInfo{Duration: duration, DisplayAspectRatio: displayAspectRatio}, nil
}

func parseSampleAspectRatio(value string) float64 {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 1
	}
	numerator, numeratorErr := strconv.ParseFloat(parts[0], 64)
	denominator, denominatorErr := strconv.ParseFloat(parts[1], 64)
	if numeratorErr != nil || denominatorErr != nil || numerator <= 0 || denominator <= 0 {
		return 1
	}

	return numerator / denominator
}

func rotationDegrees(tagValue string, sideData []probeSideData) float64 {
	for _, data := range sideData {
		if data.Rotation != 0 {
			return data.Rotation
		}
	}
	rotation, err := strconv.ParseFloat(tagValue, 64)
	if err != nil {
		return 0
	}

	return rotation
}

func isQuarterTurn(rotation float64) bool {
	normalized := math.Mod(math.Abs(rotation), 180)
	return math.Abs(normalized-90) < 0.5
}

func (t *Transcoder) probeHasAudio(ctx context.Context, inputURL string) (bool, error) {
	cmd := exec.CommandContext(ctx, t.cfg.FfprobeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "a",
		inputURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("ffprobe audio: %w", err)
	}

	return strings.Contains(string(out), `"codec_name"`), nil
}

func (t *Transcoder) Run(ctx context.Context, inputURL string, qualities []string, outputDir string, duration float64, thumbnailOptions *ThumbnailOptions, onProgress ProgressCallback) error {
	renditions := make([]quality.Rendition, 0, len(qualities))
	for _, q := range qualities {
		r, ok := quality.Get(q)
		if !ok {
			return fmt.Errorf("unknown quality %q", q)
		}
		renditions = append(renditions, r)
	}

	if len(renditions) == 0 {
		return fmt.Errorf("no valid renditions")
	}

	hasAudio, err := t.probeHasAudio(ctx, inputURL)
	if err != nil {
		t.log.Warn("audio probe failed, assuming audio present", "err", err)
		hasAudio = true
	}

	args, err := t.buildArgs(inputURL, renditions, outputDir, hasAudio, thumbnailOptions)
	if err != nil {
		return err
	}

	for i := range renditions {
		if err := os.MkdirAll(filepath.Join(outputDir, fmt.Sprintf("v%d", i)), 0o755); err != nil {
			return fmt.Errorf("create rendition directory: %w", err)
		}
	}

	t.log.Info("starting ffmpeg", "args_summary", summarizeArgs(args), "has_audio", hasAudio, "generate_thumbnail", thumbnailOptions != nil)

	cmd := exec.CommandContext(ctx, t.cfg.FfmpegBin, args...)
	cmd.Stderr = newRingBuffer(200)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	if duration <= 0 {
		duration = 1
	}

	var lastWrite time.Time
	var lastPercent int

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "out_time_us=") {
			continue
		}

		usStr := strings.TrimPrefix(line, "out_time_us=")
		us, err := strconv.ParseInt(usStr, 10, 64)
		if err != nil {
			continue
		}

		pct := int(float64(us) / 1_000_000.0 / duration * 100)
		if pct > 99 {
			pct = 99
		}

		now := time.Now()
		if pct != lastPercent && now.Sub(lastWrite) >= time.Duration(t.cfg.ProgressMinIntervalMs)*time.Millisecond {
			if onProgress != nil {
				onProgress(pct)
			}
			lastPercent = pct
			lastWrite = now
		}
	}

	if err := cmd.Wait(); err != nil {
		stderr := cmd.Stderr.(*ringBuffer).String()
		return fmt.Errorf("ffmpeg exited: %w\nlast stderr:\n%s", err, stderr)
	}

	if onProgress != nil {
		onProgress(100)
	}

	return nil
}

func (t *Transcoder) buildArgs(inputURL string, renditions []quality.Rendition, outputDir string, hasAudio bool, thumbnailOptions *ThumbnailOptions) ([]string, error) {
	n := len(renditions)
	if n == 0 {
		return nil, fmt.Errorf("no valid renditions")
	}
	args := []string{
		"-hide_banner", "-y",
		"-i", inputURL,
	}

	splitCount := n
	labels := make([]string, 0, n+1)
	for i := range renditions {
		labels = append(labels, fmt.Sprintf("[v%d]", i))
	}
	if thumbnailOptions != nil {
		splitCount += 2
		labels = append(labels, "[vthumb]", "[vposter]")
	}

	filterParts := make([]string, 0, splitCount)
	for i, rendition := range renditions {
		filterParts = append(filterParts, fmt.Sprintf("[v%d]scale=w=%d:h=%d[vout%d]", i, rendition.Width, rendition.Height, i))
	}
	if thumbnailOptions != nil {
		if thumbnailOptions.StoryboardOutputPath == "" || thumbnailOptions.PosterOutputPath == "" ||
			thumbnailOptions.PosterWidth < 1 || thumbnailOptions.PosterHeight < 1 ||
			thumbnailOptions.JPEGQuality < 2 || thumbnailOptions.JPEGQuality > 31 ||
			thumbnailOptions.Config.Width < 1 || thumbnailOptions.Config.Height < 1 ||
			thumbnailOptions.Plan.Columns < 1 || thumbnailOptions.Plan.Rows < 1 ||
			thumbnailOptions.Plan.StoryboardCellCount < 1 || thumbnailOptions.Plan.Columns*thumbnailOptions.Plan.Rows < thumbnailOptions.Plan.StoryboardCellCount ||
			thumbnailOptions.Plan.EffectiveInterval <= 0 {
			return nil, fmt.Errorf("invalid thumbnail output options")
		}
		interval := strconv.FormatFloat(thumbnailOptions.Plan.EffectiveInterval, 'f', 6, 64)
		filterParts = append(filterParts, fmt.Sprintf(
			"[vthumb]fps=1/%s,scale=w=%d:h=%d,setsar=1,tile=%dx%d:nb_frames=%d[thumbout]",
			interval,
			thumbnailOptions.Config.Width,
			thumbnailOptions.Config.Height,
			thumbnailOptions.Plan.Columns,
			thumbnailOptions.Plan.Rows,
			thumbnailOptions.Plan.StoryboardCellCount,
		))
		filterParts = append(filterParts, fmt.Sprintf(
			"[vposter]select=eq(n\\,0),scale=w=%d:h=%d,setsar=1[posterout]",
			thumbnailOptions.PosterWidth,
			thumbnailOptions.PosterHeight,
		))
	}

	filterComplex := fmt.Sprintf("[0:v]split=%d%s;%s", splitCount, strings.Join(labels, ""), strings.Join(filterParts, ";"))

	args = append(args, "-filter_complex", filterComplex)

	for i, r := range renditions {
		bufsize := r.VideoBitrate * 3 / 2
		maxrate := r.VideoBitrate * 107 / 100
		args = append(args,
			"-map", fmt.Sprintf("[vout%d]", i),
			fmt.Sprintf("-c:v:%d", i), t.cfg.FfmpegVideoCodec,
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", r.VideoBitrate),
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", maxrate),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", bufsize),
		)
	}

	if hasAudio {
		audioCodecs := []string{}
		for i, r := range renditions {
			args = append(args, "-map", "a:0")
			audioCodecs = append(audioCodecs,
				fmt.Sprintf("-c:a:%d", i), "aac",
				fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", r.AudioBitrate),
			)
		}
		args = append(args, audioCodecs...)
	}

	args = append(args,
		"-preset", "veryfast",
		"-g", "48",
		"-keyint_min", "48",
		"-sc_threshold", "0",
	)

	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(outputDir, "v%v", "segment_%d.ts"),
		"-master_pl_name", "main.m3u8",
	)

	varStreamMap := make([]string, n)
	if hasAudio {
		for i := range renditions {
			varStreamMap[i] = fmt.Sprintf("v:%d,a:%d", i, i)
		}
	} else {
		for i := range renditions {
			varStreamMap[i] = fmt.Sprintf("v:%d", i)
		}
	}
	args = append(args, "-var_stream_map", strings.Join(varStreamMap, " "))

	args = append(args, filepath.Join(outputDir, "v%v", "playlist.m3u8"))
	if thumbnailOptions != nil {
		args = append(args,
			"-map", "[thumbout]",
			"-frames:v", "1",
			"-c:v", "mjpeg",
			"-q:v", strconv.Itoa(thumbnailOptions.JPEGQuality),
			"-fps_mode", "vfr",
			thumbnailOptions.StoryboardOutputPath,
			"-map", "[posterout]",
			"-frames:v", "1",
			"-c:v", "mjpeg",
			"-q:v", strconv.Itoa(thumbnailOptions.JPEGQuality),
			"-fps_mode", "vfr",
			thumbnailOptions.PosterOutputPath,
		)
	}
	args = append(args, "-progress", "pipe:1", "-nostats")

	return args, nil
}

func summarizeArgs(args []string) string {
	safeArgs := append([]string(nil), args...)
	for index := 1; index < len(safeArgs); index++ {
		if safeArgs[index-1] == "-i" {
			safeArgs[index] = "<redacted-input>"
		}
	}

	if len(safeArgs) > 20 {
		return strings.Join(safeArgs[:10], " ") + " ... " + strings.Join(safeArgs[len(safeArgs)-5:], " ")
	}
	return strings.Join(safeArgs, " ")
}

type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRingBuffer(max int) *ringBuffer {
	return &ringBuffer{max: max}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range strings.Split(string(p), "\n") {
		if line == "" {
			continue
		}
		r.lines = append(r.lines, line)
		if len(r.lines) > r.max {
			r.lines = r.lines[len(r.lines)-r.max:]
		}
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}
