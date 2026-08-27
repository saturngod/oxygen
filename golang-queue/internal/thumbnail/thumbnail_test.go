package thumbnail

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func defaultConfig() Config {
	return Config{
		IntervalSeconds: 10,
		Width:           160,
		Height:          90,
		Columns:         10,
		Rows:            10,
	}
}

func TestHeightForAspectRatio(t *testing.T) {
	tests := []struct {
		name        string
		aspectRatio float64
		wantHeight  int
	}{
		{name: "landscape 16:9", aspectRatio: 16.0 / 9.0, wantHeight: 90},
		{name: "standard 4:3", aspectRatio: 4.0 / 3.0, wantHeight: 120},
		{name: "portrait 9:16", aspectRatio: 9.0 / 16.0, wantHeight: 284},
		{name: "square", aspectRatio: 1, wantHeight: 160},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height, err := HeightForAspectRatio(160, tt.aspectRatio)
			if err != nil {
				t.Fatal(err)
			}
			if height != tt.wantHeight {
				t.Fatalf("height = %d, want %d", height, tt.wantHeight)
			}
			if height%2 != 0 {
				t.Fatalf("height must be even, got %d", height)
			}
		})
	}
}

func TestHeightForAspectRatioRejectsInvalidValues(t *testing.T) {
	for _, aspectRatio := range []float64{0, -1, math.NaN(), math.Inf(1), math.SmallestNonzeroFloat64} {
		if _, err := HeightForAspectRatio(160, aspectRatio); err == nil {
			t.Fatalf("expected aspect ratio %v to fail", aspectRatio)
		}
	}
}

func TestBuildPlan(t *testing.T) {
	tests := []struct {
		name         string
		duration     float64
		wantCells    int
		wantColumns  int
		wantRows     int
		wantInterval float64
	}{
		{name: "partial final interval", duration: 25, wantCells: 3, wantColumns: 3, wantRows: 1, wantInterval: 10},
		{name: "short video", duration: 5, wantCells: 1, wantColumns: 1, wantRows: 1, wantInterval: 10},
		{name: "capacity boundary", duration: 1000, wantCells: 100, wantColumns: 10, wantRows: 10, wantInterval: 10},
		{name: "adaptive interval", duration: 3600, wantCells: 100, wantColumns: 10, wantRows: 10, wantInterval: 36},
		{name: "more than 24 hours", duration: 90061.2, wantCells: 100, wantColumns: 10, wantRows: 10, wantInterval: 900.612},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := BuildPlan(tt.duration, defaultConfig())
			if err != nil {
				t.Fatalf("BuildPlan() error = %v", err)
			}
			if plan.StoryboardCellCount != tt.wantCells || plan.Columns != tt.wantColumns || plan.Rows != tt.wantRows {
				t.Fatalf("BuildPlan() = %+v", plan)
			}
			if math.Abs(plan.EffectiveInterval-tt.wantInterval) > 0.000001 {
				t.Fatalf("effective interval = %f, want %f", plan.EffectiveInterval, tt.wantInterval)
			}
		})
	}
}

func TestGenerateVTT(t *testing.T) {
	cfg := defaultConfig()
	plan, err := BuildPlan(25, cfg)
	if err != nil {
		t.Fatal(err)
	}

	content, err := GenerateVTT(25, cfg, plan)
	if err != nil {
		t.Fatal(err)
	}

	wantParts := []string{
		"WEBVTT\n\n",
		"00:00:00.000 --> 00:00:10.000\nstoryboard.jpg#xywh=0,0,160,90",
		"00:00:10.000 --> 00:00:20.000\nstoryboard.jpg#xywh=160,0,160,90",
		"00:00:20.000 --> 00:00:25.000\nstoryboard.jpg#xywh=320,0,160,90",
	}
	for _, want := range wantParts {
		if !strings.Contains(content, want) {
			t.Errorf("VTT does not contain %q:\n%s", want, content)
		}
	}
	if strings.Count(content, "storyboard.jpg#xywh=") != 3 {
		t.Fatalf("expected 3 cues:\n%s", content)
	}
}

func TestGenerateVTTCoordinatesAndLongTimestamps(t *testing.T) {
	cfg := defaultConfig()
	plan, err := BuildPlan(90061.2, cfg)
	if err != nil {
		t.Fatal(err)
	}

	content, err := GenerateVTT(90061.2, cfg, plan)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(content, "storyboard.jpg#xywh=") != 100 {
		t.Fatalf("expected 100 cues")
	}
	if !strings.Contains(content, "storyboard.jpg#xywh=480,180,160,90") {
		t.Errorf("missing coordinates for cell 23")
	}
	if !strings.Contains(content, "25:01:01.200") {
		t.Errorf("timestamp wrapped or lost precision")
	}
	if !strings.Contains(content, "storyboard.jpg#xywh=1440,810,160,90") {
		t.Errorf("missing coordinates for final cell")
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := map[float64]string{
		0:       "00:00:00.000",
		64.325:  "00:01:04.325",
		3725:    "01:02:05.000",
		90061.2: "25:01:01.200",
	}

	for seconds, want := range tests {
		if got := formatTimestamp(seconds); got != want {
			t.Errorf("formatTimestamp(%f) = %q, want %q", seconds, got, want)
		}
	}
}

func TestBuildPlanRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		cfg      Config
	}{
		{name: "zero duration", duration: 0, cfg: defaultConfig()},
		{name: "negative duration", duration: -1, cfg: defaultConfig()},
		{name: "NaN duration", duration: math.NaN(), cfg: defaultConfig()},
		{name: "infinite duration", duration: math.Inf(1), cfg: defaultConfig()},
		{name: "zero interval", duration: 10, cfg: Config{Width: 160, Height: 90, Columns: 10, Rows: 10}},
		{name: "zero grid", duration: 10, cfg: Config{IntervalSeconds: 10, Width: 160, Height: 90}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildPlan(tt.duration, tt.cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateAndWriteVTT(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StoryboardFilename), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	plan, err := BuildPlan(5, cfg)
	if err != nil {
		t.Fatal(err)
	}

	size, err := ValidateAndWriteVTT(dir, 5, cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 {
		t.Fatalf("storyboard size = %d, want 4", size)
	}
	content, err := os.ReadFile(filepath.Join(dir, VTTFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "WEBVTT\n\n") {
		t.Fatalf("invalid VTT: %q", content)
	}
}

func TestValidateAndWriteVTTRejectsAdditionalJPEG(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{StoryboardFilename, "storyboard-2.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := defaultConfig()
	plan, err := BuildPlan(5, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndWriteVTT(dir, 5, cfg, plan); err == nil {
		t.Fatal("expected additional JPEG to be rejected")
	}
}

func TestValidatePoster(t *testing.T) {
	path := filepath.Join(t.TempDir(), PosterFilename)
	if err := os.WriteFile(path, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	size, err := ValidatePoster(path)
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 {
		t.Fatalf("poster size = %d, want 4", size)
	}
}
