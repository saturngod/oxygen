package server

import (
	"path/filepath"
	"strings"
	"testing"

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
	)
	joined := strings.Join(args, " ")

	assertContains(t, joined, "[0:v]split=2[v0][v1]")
	assertContains(t, joined, "[v0]scale=w=640:h=360[vout0]")
	assertContains(t, joined, "[v1]scale=w=1280:h=720[vout1]")
	assertContains(t, joined, "-var_stream_map v:0,a:0 v:1,a:1")
	assertContains(t, joined, "-master_pl_name index.m3u8")
	assertContains(t, joined, filepath.Join(outputDir, "v%v", "playlist.m3u8"))
}

func TestBuildLiveFFmpegArgsSupportsVideoOnlySingleRendition(t *testing.T) {
	render240p, _ := quality.Get("240p")
	joined := strings.Join(buildLiveFFmpegArgs(
		"rtmp://127.0.0.1:1234/live/source",
		t.TempDir(),
		[]quality.Rendition{render240p},
		false,
		"h264_videotoolbox",
	), " ")

	assertContains(t, joined, "[0:v]scale=w=352:h=240[vout0]")
	assertContains(t, joined, "-c:v:0 h264_videotoolbox")
	assertContains(t, joined, "-var_stream_map v:0")
	if strings.Contains(joined, "-c:a:") {
		t.Fatalf("video-only args unexpectedly contain audio encoding: %s", joined)
	}
}

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q to contain %q", value, expected)
	}
}
