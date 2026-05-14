package transcode

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"oxygen/worker/internal/config"
	"oxygen/worker/internal/quality"
)

type Transcoder struct {
	cfg *config.Config
	log *slog.Logger
}

type ProgressCallback func(percent int)

func NewTranscoder(cfg *config.Config) *Transcoder {
	return &Transcoder{
		cfg: cfg,
		log: slog.With("component", "transcode"),
	}
}

func (t *Transcoder) ProbeDuration(ctx context.Context, inputURL string) (float64, error) {
	cmd := exec.CommandContext(ctx, t.cfg.FfprobeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		inputURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	durationStr := extractJSONValue(string(out), "duration")
	if durationStr == "" {
		return 0, fmt.Errorf("ffprobe: no duration in output")
	}

	d, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", durationStr, err)
	}

	return d, nil
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

func (t *Transcoder) Run(ctx context.Context, inputURL string, qualities []string, outputDir string, onProgress ProgressCallback) error {
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

	n := len(renditions)
	args := []string{
		"-hide_banner", "-y",
		"-i", inputURL,
	}

	filterParts := make([]string, n)
	for i, r := range renditions {
		filterParts[i] = fmt.Sprintf("[v%d]scale=w=%d:h=%d[vout%d]", i, r.Width, r.Height, i)
	}

	filterComplex := fmt.Sprintf("[0:v]split=%d%s", n,
		func() string {
			labels := make([]string, n)
			for i := range renditions {
				labels[i] = fmt.Sprintf("[v%d]", i)
			}
			return strings.Join(labels, "")
		}()+";",
	) + strings.Join(filterParts, ";")

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
	args = append(args, "-progress", "pipe:1", "-nostats")

	for i := range renditions {
		os.MkdirAll(filepath.Join(outputDir, fmt.Sprintf("v%d", i)), 0o755)
	}

	t.log.Info("starting ffmpeg", "args_summary", summarizeArgs(args), "has_audio", hasAudio)

	cmd := exec.CommandContext(ctx, t.cfg.FfmpegBin, args...)
	cmd.Stderr = newRingBuffer(200)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	duration, _ := t.ProbeDuration(ctx, inputURL)
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

func summarizeArgs(args []string) string {
	if len(args) > 20 {
		return strings.Join(args[:10], " ") + " ... " + strings.Join(args[len(args)-5:], " ")
	}
	return strings.Join(args, " ")
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

func extractJSONValue(jsonStr, key string) string {
	needle := `"` + key + `"`
	idx := strings.Index(jsonStr, needle)
	if idx < 0 {
		return ""
	}
	after := jsonStr[idx+len(needle):]
	colon := strings.Index(after, ":")
	if colon < 0 {
		return ""
	}
	after = after[colon+1:]
	after = strings.TrimLeft(after, " \t\n\r\"")
	end := strings.Index(after, `"`)
	if end < 0 {
		end = strings.IndexAny(after, ",}\n")
	}
	if end < 0 {
		return after
	}
	return after[:end]
}
