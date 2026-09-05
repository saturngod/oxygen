package transcode

import (
	"path/filepath"
	"strings"
	"testing"

	"oxygen/worker/internal/config"
	"oxygen/worker/internal/quality"
	"oxygen/worker/internal/thumbnail"
)

func testTranscoder() *Transcoder {
	return NewTranscoder(&config.Config{FfmpegVideoCodec: "libx264"})
}

func testRenditions(t *testing.T) []quality.Rendition {
	t.Helper()
	rendition, ok := quality.Get("240p")
	if !ok {
		t.Fatal("240p rendition missing")
	}
	return []quality.Rendition{rendition}
}

func TestBuildArgsWithoutThumbnailsPreservesHLSOnlyGraph(t *testing.T) {
	args, err := testTranscoder().buildArgs("input.mp4", testRenditions(t), "/tmp/hls", false, 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "[0:v]split=1[v0];[v0]scale=w=352:h=240[vout0]") {
		t.Fatalf("unexpected filter graph: %s", joined)
	}
	for _, unwanted := range []string{"vthumb", "thumbout", "storyboard.jpg", "vposter", "posterout", "thumbnail.jpg", "tile="} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("disabled arguments contain %q: %s", unwanted, joined)
		}
	}
}

func TestBuildArgsWithSingleStoryboardOutput(t *testing.T) {
	cfg := thumbnail.Config{IntervalSeconds: 10, Width: 160, Height: 90, Columns: 10, Rows: 10}
	plan, err := thumbnail.BuildPlan(25, cfg)
	if err != nil {
		t.Fatal(err)
	}
	storyboardPath := filepath.Join(t.TempDir(), thumbnail.StoryboardFilename)
	posterPath := filepath.Join(t.TempDir(), thumbnail.PosterFilename)
	options := &ThumbnailOptions{
		StoryboardOutputPath: storyboardPath,
		PosterOutputPath:     posterPath,
		PosterWidth:          960,
		PosterHeight:         540,
		Config:               cfg,
		Plan:                 plan,
		JPEGQuality:          5,
	}

	args, err := testTranscoder().buildArgs("input.mp4", testRenditions(t), "/tmp/hls", true, 6, options)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")

	wanted := []string{
		"[0:v]split=3[v0][vthumb][vposter]",
		"fps=1/10.000000",
		"scale=w=160:h=90,setsar=1",
		"tile=3x1:nb_frames=3[thumbout]",
		"[vposter]select=eq(n\\,0),scale=w=960:h=540,setsar=1[posterout]",
		"-map [thumbout] -frames:v 1 -c:v mjpeg -q:v 5 -fps_mode vfr " + storyboardPath,
		"-map [posterout] -frames:v 1 -c:v mjpeg -q:v 5 -fps_mode vfr " + posterPath,
		"v%v/playlist.m3u8",
	}
	for _, want := range wanted {
		if !strings.Contains(joined, want) {
			t.Errorf("arguments do not contain %q:\n%s", want, joined)
		}
	}
	if strings.Count(joined, storyboardPath) != 1 {
		t.Fatalf("expected exactly one storyboard output: %s", joined)
	}
	if strings.Count(joined, posterPath) != 1 {
		t.Fatalf("expected exactly one poster output: %s", joined)
	}
}

func TestBuildArgsRejectsNoRenditions(t *testing.T) {
	if _, err := testTranscoder().buildArgs("input.mp4", nil, "/tmp/hls", false, 6, nil); err == nil {
		t.Fatal("expected no-renditions error")
	}
}

func TestBuildArgsUsesProfileSegmentDuration(t *testing.T) {
	args, err := testTranscoder().buildArgs("input.mp4", testRenditions(t), "/tmp/hls", false, 9, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")

	for _, want := range []string{"-hls_time 9", "-force_key_frames:v:0 expr:gte(t,n_forced*9)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("arguments do not contain %q: %s", want, joined)
		}
	}
}

func TestBuildArgsDefaultsInvalidSegmentDuration(t *testing.T) {
	args, err := testTranscoder().buildArgs("input.mp4", testRenditions(t), "/tmp/hls", false, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-hls_time 6") {
		t.Fatalf("invalid duration did not use the compatibility default: %s", joined)
	}
}

func TestSummarizeArgsRedactsInputURL(t *testing.T) {
	summary := summarizeArgs([]string{"-hide_banner", "-i", "https://example.test/video.mp4?secret=token", "output.m3u8"})
	if strings.Contains(summary, "secret=token") || !strings.Contains(summary, "<redacted-input>") {
		t.Fatalf("input URL was not redacted: %s", summary)
	}
}

func TestMediaAspectRatioHelpers(t *testing.T) {
	if got := parseSampleAspectRatio("4:3"); got != 4.0/3.0 {
		t.Fatalf("sample aspect ratio = %f", got)
	}
	if got := parseSampleAspectRatio("N/A"); got != 1 {
		t.Fatalf("invalid sample aspect ratio fallback = %f", got)
	}
	if got := rotationDegrees("90", nil); got != 90 {
		t.Fatalf("rotation tag = %f", got)
	}
	if got := rotationDegrees("0", []probeSideData{{Rotation: -90}}); got != -90 {
		t.Fatalf("side-data rotation = %f", got)
	}
	if !isQuarterTurn(-90) || !isQuarterTurn(270) || isQuarterTurn(180) {
		t.Fatal("quarter-turn rotation detection is incorrect")
	}
}
