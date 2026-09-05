package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortmplib"
	rtmpcodecs "github.com/bluenviron/gortmplib/pkg/codecs"

	"oxygen/live/internal/quality"
)

const ffmpegConnectTimeout = 10 * time.Second

type adaptiveHLS struct {
	cancel    context.CancelFunc
	listener  net.Listener
	relayConn net.Conn
	command   *exec.Cmd
	done      <-chan error
	failure   chan error
	failOnce  sync.Once
	publisher net.Conn
}

func (output *adaptiveHLS) reportFailure(err error) {
	if err == nil {
		return
	}

	output.failOnce.Do(func() {
		output.failure <- err
		_ = output.publisher.Close()
	})
}

func (output *adaptiveHLS) currentFailure() error {
	select {
	case err := <-output.failure:
		return err
	default:
		return nil
	}
}

func (output *adaptiveHLS) close() {
	output.cancel()
	_ = output.listener.Close()
	if output.relayConn != nil {
		_ = output.relayConn.Close()
	}

	select {
	case <-output.done:
	case <-time.After(5 * time.Second):
		if output.command.Process != nil {
			_ = output.command.Process.Kill()
		}
	}
}

func (s *Server) startAdaptiveHLS(
	ctx context.Context,
	reader *gortmplib.Reader,
	qualities []string,
	hlsDir string,
	publicID string,
	publisherConn net.Conn,
) (*adaptiveHLS, error) {
	renditions := make([]quality.Rendition, 0, len(qualities))
	for _, value := range qualities {
		rendition, ok := quality.Get(value)
		if !ok {
			return nil, fmt.Errorf("unknown live quality %q", value)
		}
		renditions = append(renditions, rendition)
	}

	hasAudio := hasAudioTrack(reader.Tracks())
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start ffmpeg relay listener: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	inputURL := "rtmp://" + listener.Addr().String() + "/live/source"
	mediaPrefix := randomHex(8)
	args := buildLiveFFmpegArgs(inputURL, hlsDir, renditions, hasAudio, s.cfg.FFmpegVideoCodec, mediaPrefix)
	command := exec.CommandContext(runCtx, s.cfg.FFmpegBin, args...)
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		cancel()
		_ = listener.Close()
		return nil, fmt.Errorf("start live ffmpeg: %w", err)
	}

	done := make(chan error, 1)
	output := &adaptiveHLS{
		cancel:    cancel,
		listener:  listener,
		command:   command,
		done:      done,
		failure:   make(chan error, 1),
		publisher: publisherConn,
	}

	go func() {
		err := command.Wait()
		if runCtx.Err() == nil {
			if err == nil {
				err = fmt.Errorf("ffmpeg exited unexpectedly")
			} else {
				err = fmt.Errorf("ffmpeg exited unexpectedly: %w", err)
			}
			output.reportFailure(err)
		}
		done <- err
	}()

	if tcpListener, ok := listener.(*net.TCPListener); ok {
		_ = tcpListener.SetDeadline(time.Now().Add(ffmpegConnectTimeout))
	}

	relayConn, err := listener.Accept()
	if err != nil {
		output.close()
		return nil, fmt.Errorf("accept ffmpeg relay: %w", err)
	}
	output.relayConn = relayConn
	_ = listener.Close()

	_ = relayConn.SetDeadline(time.Now().Add(ffmpegConnectTimeout))
	serverConn := &gortmplib.ServerConn{RW: relayConn}
	if err := serverConn.Initialize(); err != nil {
		output.close()
		return nil, fmt.Errorf("initialize ffmpeg relay: %w", err)
	}
	if err := serverConn.Accept(); err != nil {
		output.close()
		return nil, fmt.Errorf("accept ffmpeg playback: %w", err)
	}
	if serverConn.Publish {
		output.close()
		return nil, fmt.Errorf("ffmpeg relay unexpectedly published")
	}
	_ = relayConn.SetReadDeadline(time.Time{})

	writer := &gortmplib.Writer{Conn: serverConn, Tracks: reader.Tracks()}
	if err := writer.Initialize(); err != nil {
		output.close()
		return nil, fmt.Errorf("initialize ffmpeg relay writer: %w", err)
	}

	stats := &relayStats{}
	logDiagnostics := func() {
		latest, found := latestAdaptiveSegment(hlsDir)
		s.log.Info("adaptive live media diagnostics", append(stats.snapshot(), "public_id", publicID, "segment_found", found, "latest_segment_at", latest)...)
	}
	wireRTMPToWriter(reader, writer, s.log, publicID, relayConn, s.cfg.FFmpegWriteTimeout, output.reportFailure, stats)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				logDiagnostics()
			}
		}
	}()
	go watchAdaptiveHLS(runCtx, output, hlsDir, s.cfg.FFmpegStallTimeout)
	s.log.Info("adaptive live transcoder started", "public_id", publicID, "qualities", qualities)

	return output, nil
}

func buildLiveFFmpegArgs(
	inputURL string,
	outputDir string,
	renditions []quality.Rendition,
	hasAudio bool,
	videoCodec string,
	mediaPrefix string,
) []string {
	args := []string{"-hide_banner", "-loglevel", "info", "-nostats", "-y", "-i", inputURL}

	filterParts := make([]string, 0, len(renditions)+1)
	if len(renditions) == 1 {
		filterParts = append(filterParts, fmt.Sprintf("[0:v]scale=w=%d:h=%d[vout0]", renditions[0].Width, renditions[0].Height))
	} else {
		labels := make([]string, len(renditions))
		for index := range renditions {
			labels[index] = fmt.Sprintf("[v%d]", index)
		}
		filterParts = append(filterParts, fmt.Sprintf("[0:v]split=%d%s", len(renditions), strings.Join(labels, "")))
		for index, rendition := range renditions {
			filterParts = append(filterParts, fmt.Sprintf("[v%d]scale=w=%d:h=%d[vout%d]", index, rendition.Width, rendition.Height, index))
		}
	}
	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))

	for index, rendition := range renditions {
		maxrate := rendition.VideoBitrate * 107 / 100
		bufsize := rendition.VideoBitrate * 3 / 2
		args = append(args,
			"-map", fmt.Sprintf("[vout%d]", index),
			fmt.Sprintf("-c:v:%d", index), videoCodec,
			fmt.Sprintf("-b:v:%d", index), fmt.Sprintf("%dk", rendition.VideoBitrate),
			fmt.Sprintf("-maxrate:v:%d", index), fmt.Sprintf("%dk", maxrate),
			fmt.Sprintf("-bufsize:v:%d", index), fmt.Sprintf("%dk", bufsize),
		)
	}

	if hasAudio {
		for index, rendition := range renditions {
			args = append(args,
				"-map", "0:a:0",
				fmt.Sprintf("-c:a:%d", index), "aac",
				fmt.Sprintf("-b:a:%d", index), fmt.Sprintf("%dk", rendition.AudioBitrate),
			)
		}
	}

	initFilename := mediaPrefix + "_init.mp4"
	if len(renditions) > 1 {
		initFilename = mediaPrefix + "_init_%v.mp4"
	}

	args = append(args,
		"-preset", "veryfast",
		"-tune", "zerolatency",
		"-g", "60",
		"-keyint_min", "60",
		"-sc_threshold", "0",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "6",
		"-hls_delete_threshold", "2",
		"-hls_segment_type", "fmp4",
		"-hls_flags", "delete_segments+independent_segments+temp_file",
		"-hls_fmp4_init_filename", initFilename,
		"-hls_segment_filename", filepath.Join(outputDir, "v%v", mediaPrefix+"_segment_%09d.m4s"),
		"-master_pl_name", "index.m3u8",
	)

	variantMap := make([]string, len(renditions))
	for index := range renditions {
		variantMap[index] = fmt.Sprintf("v:%d", index)
		if hasAudio {
			variantMap[index] += fmt.Sprintf(",a:%d", index)
		}
		_ = os.MkdirAll(filepath.Join(outputDir, fmt.Sprintf("v%d", index)), 0o755)
	}

	return append(args,
		"-var_stream_map", strings.Join(variantMap, " "),
		filepath.Join(outputDir, "v%v", "playlist.m3u8"),
	)
}

func hasAudioTrack(tracks []*gortmplib.Track) bool {
	for _, track := range tracks {
		if !track.Codec.IsVideo() {
			return true
		}
	}

	return false
}

func wireRTMPToWriter(
	reader *gortmplib.Reader,
	writer *gortmplib.Writer,
	log logger,
	publicID string,
	relayConn net.Conn,
	writeTimeout time.Duration,
	reportFailure func(error),
	stats *relayStats,
) {
	var writerMu sync.Mutex

	write := func(callback func() error) {
		writerMu.Lock()
		err := relayConn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err == nil {
			err = callback()
		}
		writerMu.Unlock()
		if err == nil {
			stats.mu.Lock()
			stats.forwarded++
			stats.mu.Unlock()
		}
		if err != nil {
			log.Warn("ffmpeg relay write failed", "err", err, "public_id", publicID)
			reportFailure(fmt.Errorf("ffmpeg relay write failed: %w", err))
		}
	}

	for _, track := range reader.Tracks() {
		switch codec := track.Codec.(type) {
		case *rtmpcodecs.H264:
			filter := h264RelayFilter{sps: codec.SPS, pps: codec.PPS}
			reader.OnDataH264(track, func(pts time.Duration, dts time.Duration, au [][]byte) {
				stats.record("video", dts)
				au = filter.process(au)
				if au == nil {
					return
				}
				write(func() error { return writer.WriteH264(track, pts, dts, au) })
			})
		case *rtmpcodecs.H265:
			reader.OnDataH265(track, func(pts time.Duration, dts time.Duration, au [][]byte) {
				stats.record("video", dts)
				write(func() error { return writer.WriteH265(track, pts, dts, au) })
			})
		case *rtmpcodecs.MPEG4Audio:
			reader.OnDataMPEG4Audio(track, func(pts time.Duration, accessUnit []byte) {
				stats.record("audio", pts)
				write(func() error { return writer.WriteMPEG4Audio(track, pts, accessUnit) })
			})
		case *rtmpcodecs.Opus:
			reader.OnDataOpus(track, func(pts time.Duration, packet []byte) {
				stats.record("audio", pts)
				write(func() error { return writer.WriteOpus(track, pts, packet) })
			})
		}
	}
}

// Reader callbacks include sequence headers containing only SPS/PPS. These are
// codec configuration, not pictures; forwarding them as RTMP video AUs gives
// FFmpeg empty frames and can introduce a timestamp unrelated to the media.
type h264RelayFilter struct {
	sps []byte
	pps []byte
}

func (filter *h264RelayFilter) process(au [][]byte) [][]byte {
	hasPicture := false
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch nalu[0] & 0x1f {
		case 1, 2, 3, 4, 5:
			hasPicture = true
		case 7:
			filter.sps = append([]byte(nil), nalu...)
		case 8:
			filter.pps = append([]byte(nil), nalu...)
		}
	}
	if !hasPicture {
		return nil
	}
	return withH264ParameterSets(au, filter.sps, filter.pps)
}

func watchAdaptiveHLS(ctx context.Context, output *adaptiveHLS, root string, timeout time.Duration) {
	interval := timeout / 3
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}

	startedAt := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			latest, found := latestAdaptiveSegment(root)
			if (!found && now.Sub(startedAt) > timeout) || (found && now.Sub(latest) > timeout) {
				output.reportFailure(fmt.Errorf("adaptive HLS output stalled for more than %s", timeout))
				return
			}
		}
	}
}

func latestAdaptiveSegment(root string) (time.Time, bool) {
	variants, err := os.ReadDir(root)
	if err != nil {
		return time.Time{}, false
	}

	var latest time.Time
	found := false
	for _, variant := range variants {
		if !variant.IsDir() || !strings.HasPrefix(variant.Name(), "v") {
			continue
		}

		entries, err := os.ReadDir(filepath.Join(root, variant.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".m4s") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if !found || info.ModTime().After(latest) {
				latest = info.ModTime()
				found = true
			}
		}
	}

	return latest, found
}
