package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortmplib"
	rtmpcodecs "github.com/bluenviron/gortmplib/pkg/codecs"

	"oxygen/live/internal/quality"
)

const ffmpegConnectTimeout = 10 * time.Second

type adaptiveHLS struct {
	cancel     context.CancelFunc
	listener   net.Listener
	relayConn  net.Conn
	command    *exec.Cmd
	done       <-chan struct{}
	failureMu  sync.RWMutex
	failureErr error
	failOnce   sync.Once
	closeOnce  sync.Once
	publisher  net.Conn
}

func (output *adaptiveHLS) reportFailure(err error) {
	if err == nil {
		return
	}

	output.failOnce.Do(func() {
		output.failureMu.Lock()
		output.failureErr = err
		output.failureMu.Unlock()
		_ = output.publisher.Close()
	})
}

func (output *adaptiveHLS) currentFailure() error {
	output.failureMu.RLock()
	defer output.failureMu.RUnlock()

	return output.failureErr
}

func (output *adaptiveHLS) close() {
	output.closeOnce.Do(func() {
		output.cancel()
		output.interruptRelay()

		select {
		case <-output.done:
		case <-time.After(5 * time.Second):
			if output.command.Process != nil {
				_ = output.command.Process.Kill()
			}
			<-output.done
		}
	})
}

func (output *adaptiveHLS) interruptRelay() {
	_ = output.listener.Close()
	if output.relayConn != nil {
		_ = output.relayConn.Close()
	}
}

func (output *adaptiveHLS) startWatchdog(ctx context.Context, root string, timeout time.Duration) {
	go watchAdaptiveHLS(ctx, output, root, timeout)
}

func (s *Server) startAdaptiveHLS(
	ctx context.Context,
	reader *gortmplib.Reader,
	qualities []string,
	segmentDurationSeconds int,
	hlsDir string,
	publicID string,
	publisherConn net.Conn,
	stats *relayStats,
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
	args := buildLiveFFmpegArgs(
		inputURL,
		hlsDir,
		renditions,
		hasAudio,
		s.cfg.FFmpegVideoCodec,
		mediaPrefix,
		s.cfg.FFmpegAnalyzeDuration,
		s.cfg.FFmpegProbeSize,
		segmentDurationSeconds,
	)
	command := exec.CommandContext(runCtx, s.cfg.FFmpegBin, args...)
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		cancel()
		_ = listener.Close()
		return nil, fmt.Errorf("start live ffmpeg: %w", err)
	}

	done := make(chan struct{})
	output := &adaptiveHLS{
		cancel:    cancel,
		listener:  listener,
		command:   command,
		done:      done,
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
		close(done)
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

	wireRTMPToWriter(reader, writer, s.log, publicID, relayConn, s.cfg.FFmpegWriteTimeout, output.reportFailure, stats)
	s.log.Info("adaptive live transcoder started", "public_id", publicID, "qualities", qualities, "segment_duration_seconds", segmentDurationSeconds)

	return output, nil
}

func buildLiveFFmpegArgs(
	inputURL string,
	outputDir string,
	renditions []quality.Rendition,
	hasAudio bool,
	videoCodec string,
	mediaPrefix string,
	analyzeDuration int,
	probeSize int,
	segmentDurationSeconds int,
) []string {
	segmentDurationSeconds = normalizedLiveSegmentDurationSeconds(segmentDurationSeconds)
	segmentDuration := strconv.Itoa(segmentDurationSeconds)
	playlistSegmentCount := strconv.Itoa(livePlaylistSegmentCount(segmentDurationSeconds))
	args := []string{"-hide_banner", "-loglevel", "info", "-nostats", "-y"}
	if strings.HasPrefix(inputURL, "rtmp://") || strings.HasPrefix(inputURL, "rtmps://") {
		args = append(args, "-rtmp_live", "live", "-rtmp_buffer", "0")
	}
	args = append(args,
		"-analyzeduration", fmt.Sprintf("%d", analyzeDuration),
		"-probesize", fmt.Sprintf("%d", probeSize),
		"-i", inputURL,
	)

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
			fmt.Sprintf("-g:v:%d", index), strconv.Itoa(segmentDurationSeconds*60),
			fmt.Sprintf("-force_key_frames:v:%d", index), "expr:gte(t,n_forced*"+segmentDuration+")",
		)
		if videoCodec == "libx264" {
			args = append(args,
				fmt.Sprintf("-preset:v:%d", index), "veryfast",
				fmt.Sprintf("-tune:v:%d", index), "zerolatency",
				fmt.Sprintf("-keyint_min:v:%d", index), "1",
				fmt.Sprintf("-sc_threshold:v:%d", index), "0",
				fmt.Sprintf("-bf:v:%d", index), "0",
			)
		}
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
		"-f", "hls",
		"-hls_time", segmentDuration,
		"-hls_list_size", playlistSegmentCount,
		"-hls_delete_threshold", "5",
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

func normalizedLiveSegmentDurationSeconds(segmentDurationSeconds int) int {
	if segmentDurationSeconds < 1 || segmentDurationSeconds > 30 {
		return 2
	}

	return segmentDurationSeconds
}

// Keep approximately two minutes of completed media in each live playlist so
// downstream HLS consumers can maintain a deeper buffer without increasing the
// duration (and recovery cost) of individual segments.
const livePlaylistDurationSeconds = 120

func livePlaylistSegmentCount(segmentDurationSeconds int) int {
	segmentDurationSeconds = normalizedLiveSegmentDurationSeconds(segmentDurationSeconds)

	return (livePlaylistDurationSeconds + segmentDurationSeconds - 1) / segmentDurationSeconds
}

// adaptiveMinimumReadySegments gates when an adaptive session may go live.
// The web player joins with liveSyncDurationCount = 2, i.e. two segments of
// back-buffer behind the live edge.
const adaptiveMinimumReadySegments = 2

func liveHLSTimeout(configured time.Duration, segmentDurationSeconds int) time.Duration {
	minimum := time.Duration(normalizedLiveSegmentDurationSeconds(segmentDurationSeconds)*3+10) * time.Second
	if configured < minimum {
		return minimum
	}

	return configured
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
				au = filter.process(au)
				if au == nil {
					return
				}
				stats.recordVideo(dts, h264HasIDR(au))
				write(func() error { return writer.WriteH264(track, pts, dts, au) })
			})
		case *rtmpcodecs.H265:
			reader.OnDataH265(track, func(pts time.Duration, dts time.Duration, au [][]byte) {
				stats.recordVideo(dts, h265HasRandomAccess(au))
				write(func() error { return writer.WriteH265(track, pts, dts, au) })
			})
		case *rtmpcodecs.MPEG4Audio:
			reader.OnDataMPEG4Audio(track, func(pts time.Duration, accessUnit []byte) {
				stats.recordAudio(pts)
				write(func() error { return writer.WriteMPEG4Audio(track, pts, accessUnit) })
			})
		case *rtmpcodecs.Opus:
			reader.OnDataOpus(track, func(pts time.Duration, packet []byte) {
				stats.recordAudio(pts)
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

	progress := make(map[string]hlsRenditionState)
	lastAdvanced := make(map[string]time.Time)
	var unavailableSince time.Time
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			state, err := inspectHLSOutput(root, 1)
			if err != nil {
				if unavailableSince.IsZero() {
					unavailableSince = now
				}
				if now.Sub(unavailableSince) > timeout {
					output.reportFailure(fmt.Errorf("adaptive HLS playlists unavailable for more than %s: %w", timeout, err))
					return
				}
				continue
			}
			unavailableSince = time.Time{}
			for name, current := range state.Renditions {
				previous, seen := progress[name]
				if !seen || current.Latest.After(previous.Latest) || current.SegmentCount != previous.SegmentCount {
					lastAdvanced[name] = now
					progress[name] = current
				}
				if last := lastAdvanced[name]; !last.IsZero() && now.Sub(last) > timeout {
					output.reportFailure(fmt.Errorf("adaptive HLS rendition %s stalled for more than %s", name, timeout))
					return
				}
			}
			for name := range progress {
				if _, exists := state.Renditions[name]; !exists && now.Sub(lastAdvanced[name]) > timeout {
					output.reportFailure(fmt.Errorf("adaptive HLS rendition %s disappeared for more than %s", name, timeout))
					return
				}
			}
		}
	}
}
