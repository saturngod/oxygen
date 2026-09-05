package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"oxygen/live/internal/quality"
)

func TestBuildLiveFFmpegArgsCreatesAdaptiveMasterPlaylist(t *testing.T) {
	render360p, _ := quality.Get("360p")
	render720p, _ := quality.Get("720p")
	outputDir := t.TempDir()

	args := buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		outputDir,
		[]quality.Rendition{render360p, render720p},
		true,
		"libx264",
		"run-a1b2",
		1000000,
		1048576,
	)
	joined := strings.Join(args, " ")

	assertContains(t, joined, "[0:v]split=2[v0][v1]")
	assertContains(t, joined, "[v0]scale=w=640:h=360[vout0]")
	assertContains(t, joined, "[v1]scale=w=1280:h=720[vout1]")
	assertContains(t, joined, "-var_stream_map v:0,a:0 v:1,a:1")
	assertContains(t, joined, "-master_pl_name index.m3u8")
	assertContains(t, joined, "-hls_fmp4_init_filename run-a1b2_init_%v.mp4")
	assertContains(t, joined, "run-a1b2_segment_%09d.m4s")
	assertContains(t, joined, "-rtmp_live live -rtmp_buffer 0 -analyzeduration 1000000 -probesize 1048576 -i")
	assertContains(t, joined, "-g:v:0 60")
	assertContains(t, joined, "-bf:v:0 0")
	assertContains(t, joined, filepath.Join(outputDir, "v%v", "playlist.m3u8"))
}

func TestAdaptiveHLSWatchdogReportsMissingOutput(t *testing.T) {
	publisher, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &adaptiveHLS{
		publisher: publisher,
	}

	go watchAdaptiveHLS(ctx, output, t.TempDir(), 150*time.Millisecond)

	deadline := time.After(time.Second)
	for {
		if err := output.currentFailure(); err != nil {
			if !strings.Contains(err.Error(), "adaptive HLS") {
				t.Fatalf("unexpected watchdog failure: %v", err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("watchdog did not report missing output")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestHLSOutputReadyRequiresMasterAndEnoughSegmentsInOneRendition(t *testing.T) {
	root := t.TempDir()
	variant := filepath.Join(root, "v0")
	if err := os.MkdirAll(variant, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.m3u8"), []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nv0/playlist.m3u8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variant, "playlist.m3u8"), []byte("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2.0,\nrun_segment_000000000.m4s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variant, "init.mp4"), []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variant, "run_segment_000000000.m4s"), []byte("segment"), 0o644); err != nil {
		t.Fatal(err)
	}

	if hlsOutputReady(root, 2) {
		t.Fatal("output became ready before the rendition had enough segments")
	}

	if err := os.WriteFile(filepath.Join(variant, "run_segment_000000001.m4s"), []byte("segment"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variant, "playlist.m3u8"), []byte("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2.0,\nrun_segment_000000000.m4s\n#EXTINF:2.0,\nrun_segment_000000001.m4s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hlsOutputReady(root, 2) {
		t.Fatal("expected output with a master playlist and two segments to be ready")
	}
}

func TestHLSOutputReadyRecognizesDirectFMP4Segments(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"index.m3u8":       "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nstream.m3u8\n",
		"stream.m3u8":      "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-MAP:URI=\"stream_init.mp4\"\n#EXTINF:2.0,\nstream_seg0.mp4\n",
		"stream_init.mp4":  "init",
		"stream_seg0.mp4":  "segment",
		"stream_part0.mp4": "part",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if !hlsOutputReady(root, 1) {
		t.Fatal("expected a direct fMP4 segment to make output ready")
	}
	if hlsOutputReady(root, 2) {
		t.Fatal("initialization and partial files must not count as complete segments")
	}
	if err := os.Remove(filepath.Join(root, "stream_init.mp4")); err != nil {
		t.Fatal(err)
	}
	if hlsOutputReady(root, 1) {
		t.Fatal("output became ready without its referenced initialization file")
	}
}

func TestHLSOutputReadyRequiresEveryAdvertisedVariantAndAudioRendition(t *testing.T) {
	root := t.TempDir()
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"main\",URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000,CODECS=\"avc1.42e01e,mp4a.40.2\",AUDIO=\"audio\"\nvideo.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=500,CODECS=\"avc1.42e01e\"\nmissing.m3u8\n"
	if err := os.WriteFile(filepath.Join(root, "index.m3u8"), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestMediaPlaylist(t, root, "video", 2)
	writeTestMediaPlaylist(t, root, "audio", 2)
	if hlsOutputReady(root, 2) {
		t.Fatal("output became ready while an advertised variant was missing")
	}
	writeTestMediaPlaylist(t, root, "missing", 2)
	if !hlsOutputReady(root, 2) {
		t.Fatal("expected all advertised playlists to be ready")
	}
}

func TestHLSOutputReadyCountsSharedDirectoryPlaylistsIndependently(t *testing.T) {
	root := t.TempDir()
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"main\",URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000,CODECS=\"avc1.42e01e,mp4a.40.2\",AUDIO=\"audio\"\nvideo.m3u8\n"
	if err := os.WriteFile(filepath.Join(root, "index.m3u8"), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestMediaPlaylist(t, root, "video", 2)
	writeTestMediaPlaylist(t, root, "audio", 1)
	if hlsOutputReady(root, 2) {
		t.Fatal("video segments must not satisfy audio rendition readiness")
	}
	writeTestMediaPlaylist(t, root, "audio", 2)
	if !hlsOutputReady(root, 2) {
		t.Fatal("expected both shared-directory playlists to be ready")
	}
}

func writeTestMediaPlaylist(t *testing.T, root string, name string, segmentCount int) {
	t.Helper()
	initName := name + "_init.mp4"
	if err := os.WriteFile(filepath.Join(root, initName), []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-MAP:URI=\"")
	body.WriteString(initName)
	body.WriteString("\"\n")
	for index := range segmentCount {
		segmentName := fmt.Sprintf("%s_segment_%d.m4s", name, index)
		if err := os.WriteFile(filepath.Join(root, segmentName), []byte("segment"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "#EXTINF:2.0,\n%s\n", segmentName)
	}
	if err := os.WriteFile(filepath.Join(root, name+".m3u8"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForHLSReadyReturnsRTMPReadFailure(t *testing.T) {
	readDone := make(chan struct{})
	close(readDone)

	_, err := waitForHLSReady(context.Background(), t.TempDir(), 1, readDone, func() error {
		return fmt.Errorf("publisher disconnected")
	}, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "publisher disconnected") {
		t.Fatalf("expected publisher disconnect error, got %v", err)
	}
}

func TestWaitForHLSReadyHonorsStartupContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	readDone := make(chan struct{})

	_, err := waitForHLSReady(ctx, t.TempDir(), 1, readDone, func() error { return nil }, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected bounded startup timeout, got %v", err)
	}
}

func TestAdaptiveHLSWatchdogDetectsOneStalledRendition(t *testing.T) {
	root := t.TempDir()
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000,CODECS=\"avc1.42e01e\"\nv0.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=500,CODECS=\"avc1.42e01e\"\nv1.m3u8\n"
	if err := os.WriteFile(filepath.Join(root, "index.m3u8"), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestMediaPlaylist(t, root, "v0", 1)
	writeTestMediaPlaylist(t, root, "v1", 1)
	publisher, peer := net.Pipe()
	defer peer.Close()
	output := &adaptiveHLS{publisher: publisher}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchAdaptiveHLS(ctx, output, root, 250*time.Millisecond)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			writeTestMediaPlaylist(t, root, "v0", 1)
			if failure := output.currentFailure(); failure != nil {
				if !strings.Contains(failure.Error(), "v1.m3u8") {
					t.Fatalf("wrong rendition failed: %v", failure)
				}
				return
			}
		case <-deadline:
			t.Fatal("watchdog did not detect the stalled rendition")
		}
	}
}

func TestBuildLiveFFmpegArgsSupportsVideoOnlySingleRendition(t *testing.T) {
	render240p, _ := quality.Get("240p")
	joined := strings.Join(buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		t.TempDir(),
		[]quality.Rendition{render240p},
		false,
		"h264_videotoolbox",
		"run-c3d4",
		1000000,
		1048576,
	), " ")

	assertContains(t, joined, "[0:v]scale=w=352:h=240[vout0]")
	assertContains(t, joined, "-c:v:0 h264_videotoolbox")
	assertContains(t, joined, "-var_stream_map v:0")
	if strings.Contains(joined, "-c:a:") {
		t.Fatalf("video-only args unexpectedly contain audio encoding: %s", joined)
	}
}

func TestBuildLiveFFmpegArgsUsesDifferentMediaURLsAcrossSessions(t *testing.T) {
	render360p, _ := quality.Get("360p")
	first := strings.Join(buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		t.TempDir(),
		[]quality.Rendition{render360p},
		true,
		"libx264",
		"session-one",
		1000000,
		1048576,
	), " ")
	second := strings.Join(buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		t.TempDir(),
		[]quality.Rendition{render360p},
		true,
		"libx264",
		"session-two",
		1000000,
		1048576,
	), " ")

	assertContains(t, first, "session-one_init.mp4")
	assertContains(t, first, "session-one_segment_%09d.m4s")
	assertContains(t, second, "session-two_init.mp4")
	assertContains(t, second, "session-two_segment_%09d.m4s")
	if first == second {
		t.Fatal("expected per-session HLS media URLs")
	}
}

func TestLiveFFmpegWritesVariantInitializationFiles(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for the HLS integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	input := filepath.Join(root, "input.mp4")
	output, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=352x240:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "3", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", input).CombinedOutput()
	if err != nil {
		t.Fatalf("create input: %v\n%s", err, output)
	}
	render240p, _ := quality.Get("240p")
	render360p, _ := quality.Get("360p")
	args := buildLiveFFmpegArgs(input, root, []quality.Rendition{render240p, render360p}, true, "libx264", "integration", 1000000, 1048576)
	if output, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("transcode adaptive HLS: %v\n%s", err, output)
	}
	for index := 0; index < 2; index++ {
		variantDir := filepath.Join(root, fmt.Sprintf("v%d", index))
		initName := fmt.Sprintf("integration_init_%d.mp4", index)
		info, err := os.Stat(filepath.Join(variantDir, initName))
		if err != nil || info.Size() == 0 {
			t.Fatalf("missing or empty variant init file %s: %v", initName, err)
		}
		playlist, err := os.ReadFile(filepath.Join(variantDir, "playlist.m3u8"))
		if err != nil {
			t.Fatal(err)
		}
		assertContains(t, string(playlist), "URI=\""+initName+"\"")
		assertContains(t, string(playlist), ".m4s")
	}
}

func TestH264RelayFiltersConfigurationAndPreservesUpdatedParameterSets(t *testing.T) {
	filter := h264RelayFilter{}
	sps := []byte{0x67, 0x01}
	pps := []byte{0x68, 0x02}
	if got := filter.process([][]byte{sps, pps}); got != nil {
		t.Fatal("configuration-only callback must not become a video frame")
	}
	sps[1] = 0xff
	keyframe := []byte{0x65, 0x03}
	want := [][]byte{{0x67, 0x01}, pps, keyframe}
	if got := filter.process([][]byte{keyframe}); !reflect.DeepEqual(got, want) {
		t.Fatalf("keyframe configuration: got %v, want %v", got, want)
	}
	frame := [][]byte{{0x41, 0x04}}
	if got := filter.process(frame); !reflect.DeepEqual(got, frame) {
		t.Fatal("non-keyframe changed")
	}
	if got := filter.process([][]byte{nil, {0x06, 0x05}}); got != nil {
		t.Fatal("empty/SEI-only callback must not become a video frame")
	}
}

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q to contain %q", value, expected)
	}
}
