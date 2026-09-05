package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	)
	joined := strings.Join(args, " ")

	assertContains(t, joined, "[0:v]split=2[v0][v1]")
	assertContains(t, joined, "[v0]scale=w=640:h=360[vout0]")
	assertContains(t, joined, "[v1]scale=w=1280:h=720[vout1]")
	assertContains(t, joined, "-var_stream_map v:0,a:0 v:1,a:1")
	assertContains(t, joined, "-master_pl_name index.m3u8")
	assertContains(t, joined, "-hls_fmp4_init_filename run-a1b2_init_%v.mp4")
	assertContains(t, joined, "run-a1b2_segment_%09d.m4s")
	assertContains(t, joined, filepath.Join(outputDir, "v%v", "playlist.m3u8"))
}

func TestAdaptiveHLSWatchdogReportsMissingOutput(t *testing.T) {
	publisher, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &adaptiveHLS{
		failure:   make(chan error, 1),
		publisher: publisher,
	}

	go watchAdaptiveHLS(ctx, output, t.TempDir(), 150*time.Millisecond)

	deadline := time.After(time.Second)
	for {
		if err := output.currentFailure(); err != nil {
			if !strings.Contains(err.Error(), "stalled") {
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

func TestLatestAdaptiveSegmentFindsNewestVariantMedia(t *testing.T) {
	root := t.TempDir()
	variant := filepath.Join(root, "v0")
	if err := os.MkdirAll(variant, 0o755); err != nil {
		t.Fatal(err)
	}
	segment := filepath.Join(variant, "run_segment_000000001.m4s")
	if err := os.WriteFile(segment, []byte("segment"), 0o644); err != nil {
		t.Fatal(err)
	}

	latest, found := latestAdaptiveSegment(root)
	if !found || latest.IsZero() {
		t.Fatal("expected adaptive segment to be found")
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
	), " ")
	second := strings.Join(buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		t.TempDir(),
		[]quality.Rendition{render360p},
		true,
		"libx264",
		"session-two",
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
	args := buildLiveFFmpegArgs(input, root, []quality.Rendition{render240p, render360p}, true, "libx264", "integration")
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

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q to contain %q", value, expected)
	}
}
