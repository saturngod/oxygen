package transcode

import (
	"context"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"oxygen/worker/internal/config"
	"oxygen/worker/internal/thumbnail"
)

func TestTranscodeProducesHLSStoryboardAndPoster(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	ffprobeBin, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not available")
	}

	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.mp4")
	generateSource := exec.CommandContext(ctx, ffmpegBin,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		sourcePath,
	)
	if output, err := generateSource.CombinedOutput(); err != nil {
		t.Fatalf("local ffmpeg cannot create the integration fixture: %v: %s", err, output)
	}

	cfg := &config.Config{
		FfmpegBin:             ffmpegBin,
		FfprobeBin:            ffprobeBin,
		FfmpegVideoCodec:      "libx264",
		ProgressMinIntervalMs: 500,
	}
	transcoder := NewTranscoder(cfg)
	mediaInfo, err := transcoder.ProbeMedia(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	derivedHeight, err := thumbnail.HeightForAspectRatio(160, mediaInfo.DisplayAspectRatio)
	if err != nil {
		t.Fatal(err)
	}
	if derivedHeight != 120 {
		t.Fatalf("derived thumbnail height = %d, want 120 for a 4:3 source", derivedHeight)
	}
	thumbnailConfig := thumbnail.Config{IntervalSeconds: 1, Width: 160, Height: derivedHeight, Columns: 2, Rows: 2}
	posterHeight, err := thumbnail.HeightForAspectRatio(960, mediaInfo.DisplayAspectRatio)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := thumbnail.BuildPlan(mediaInfo.Duration, thumbnailConfig)
	if err != nil {
		t.Fatal(err)
	}
	hlsDir := filepath.Join(dir, "hls")
	thumbnailDir := filepath.Join(dir, "thumbnails")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(thumbnailDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err = transcoder.Run(ctx, sourcePath, []string{"240p"}, hlsDir, mediaInfo.Duration, 1, &ThumbnailOptions{
		StoryboardOutputPath: filepath.Join(thumbnailDir, thumbnail.StoryboardFilename),
		PosterOutputPath:     filepath.Join(dir, thumbnail.PosterFilename),
		PosterWidth:          960,
		PosterHeight:         posterHeight,
		Config:               thumbnailConfig,
		Plan:                 plan,
		JPEGQuality:          5,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(hlsDir, "main.m3u8"),
		filepath.Join(hlsDir, "v0", "playlist.m3u8"),
	} {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("expected non-empty HLS artifact %s: %v", path, err)
		}
	}
	segments, err := filepath.Glob(filepath.Join(hlsDir, "v0", "segment_*.ts"))
	if err != nil || len(segments) == 0 {
		t.Fatalf("expected HLS segments: %v", err)
	}
	if len(segments) < 2 {
		t.Fatalf("expected the one-second profile duration to create multiple segments, got %d", len(segments))
	}

	storyboards, err := filepath.Glob(filepath.Join(thumbnailDir, "*.jpg"))
	if err != nil || len(storyboards) != 1 || filepath.Base(storyboards[0]) != thumbnail.StoryboardFilename {
		t.Fatalf("expected exactly one storyboard: %v, %v", storyboards, err)
	}
	storyboard, err := os.Open(storyboards[0])
	if err != nil {
		t.Fatal(err)
	}
	dimensions, err := jpeg.DecodeConfig(storyboard)
	storyboard.Close()
	if err != nil {
		t.Fatalf("decode storyboard: %v", err)
	}
	if dimensions.Width != plan.Columns*thumbnailConfig.Width || dimensions.Height != plan.Rows*thumbnailConfig.Height {
		t.Fatalf("storyboard dimensions = %dx%d, want %dx%d", dimensions.Width, dimensions.Height, plan.Columns*thumbnailConfig.Width, plan.Rows*thumbnailConfig.Height)
	}

	posterPath := filepath.Join(dir, thumbnail.PosterFilename)
	poster, err := os.Open(posterPath)
	if err != nil {
		t.Fatal(err)
	}
	posterDimensions, err := jpeg.DecodeConfig(poster)
	poster.Close()
	if err != nil {
		t.Fatalf("decode poster: %v", err)
	}
	if posterDimensions.Width != 960 || posterDimensions.Height != 720 {
		t.Fatalf("poster dimensions = %dx%d, want 960x720", posterDimensions.Width, posterDimensions.Height)
	}
	if _, err := thumbnail.ValidatePoster(posterPath); err != nil {
		t.Fatal(err)
	}

	if _, err := thumbnail.ValidateAndWriteVTT(thumbnailDir, mediaInfo.Duration, thumbnailConfig, plan); err != nil {
		t.Fatal(err)
	}
	vtt, err := os.ReadFile(filepath.Join(thumbnailDir, thumbnail.VTTFilename))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(vtt), "storyboard.jpg#xywh=") != plan.StoryboardCellCount {
		t.Fatalf("VTT cue count does not match storyboard plan:\n%s", vtt)
	}
	if !strings.Contains(string(vtt), "storyboard.jpg#xywh=0,0,160,120") {
		t.Fatalf("VTT does not use the derived 4:3 cell height:\n%s", vtt)
	}
}
