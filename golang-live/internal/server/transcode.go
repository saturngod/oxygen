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
	args := buildLiveFFmpegArgs(inputURL, hlsDir, renditions, hasAudio, s.cfg.FFmpegVideoCodec)
	command := exec.CommandContext(runCtx, s.cfg.FFmpegBin, args...)
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		cancel()
		_ = listener.Close()
		return nil, fmt.Errorf("start live ffmpeg: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		_ = publisherConn.Close()
	}()

	output := &adaptiveHLS{
		cancel:   cancel,
		listener: listener,
		command:  command,
		done:     done,
	}

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
	_ = relayConn.SetDeadline(time.Time{})

	writer := &gortmplib.Writer{Conn: serverConn, Tracks: reader.Tracks()}
	if err := writer.Initialize(); err != nil {
		output.close()
		return nil, fmt.Errorf("initialize ffmpeg relay writer: %w", err)
	}

	wireRTMPToWriter(reader, writer, s.log, publicID, publisherConn)
	s.log.Info("adaptive live transcoder started", "public_id", publicID, "qualities", qualities)

	return output, nil
}

func buildLiveFFmpegArgs(
	inputURL string,
	outputDir string,
	renditions []quality.Rendition,
	hasAudio bool,
	videoCodec string,
) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-y", "-i", inputURL}

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
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_segment_filename", filepath.Join(outputDir, "v%v", "segment_%09d.m4s"),
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
	publisherConn net.Conn,
) {
	var writerMu sync.Mutex
	var disconnectOnce sync.Once

	write := func(callback func() error) {
		writerMu.Lock()
		err := callback()
		writerMu.Unlock()
		if err != nil {
			log.Warn("ffmpeg relay write failed", "err", err, "public_id", publicID)
			disconnectOnce.Do(func() { _ = publisherConn.Close() })
		}
	}

	for _, track := range reader.Tracks() {
		switch track.Codec.(type) {
		case *rtmpcodecs.H264:
			reader.OnDataH264(track, func(pts time.Duration, dts time.Duration, au [][]byte) {
				write(func() error { return writer.WriteH264(track, pts, dts, au) })
			})
		case *rtmpcodecs.H265:
			reader.OnDataH265(track, func(pts time.Duration, dts time.Duration, au [][]byte) {
				write(func() error { return writer.WriteH265(track, pts, dts, au) })
			})
		case *rtmpcodecs.MPEG4Audio:
			reader.OnDataMPEG4Audio(track, func(pts time.Duration, accessUnit []byte) {
				write(func() error { return writer.WriteMPEG4Audio(track, pts, accessUnit) })
			})
		case *rtmpcodecs.Opus:
			reader.OnDataOpus(track, func(pts time.Duration, packet []byte) {
				write(func() error { return writer.WriteOpus(track, pts, packet) })
			})
		}
	}
}
