package thumbnail

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	StoryboardFilename = "storyboard.jpg"
	VTTFilename        = "storyboard.vtt"
	PosterFilename     = "thumbnail.jpg"
)

type Config struct {
	IntervalSeconds float64
	Width           int
	Height          int
	Columns         int
	Rows            int
}

func ValidatePoster(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, fmt.Errorf("inspect poster: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, fmt.Errorf("poster must be a non-empty regular file")
	}

	return info.Size(), nil
}

type Plan struct {
	StoryboardCellCount int
	Columns             int
	Rows                int
	EffectiveInterval   float64
}

func HeightForAspectRatio(width int, displayAspectRatio float64) (int, error) {
	if width <= 0 || displayAspectRatio <= 0 || math.IsNaN(displayAspectRatio) || math.IsInf(displayAspectRatio, 0) {
		return 0, fmt.Errorf("thumbnail width and display aspect ratio must be valid positive values")
	}

	rawHeight := float64(width) / displayAspectRatio
	if math.IsInf(rawHeight, 0) || rawHeight > float64(math.MaxInt-1) {
		return 0, fmt.Errorf("derived thumbnail height is too large")
	}
	height := int(math.Round(rawHeight/2) * 2)
	if height < 2 {
		height = 2
	}

	return height, nil
}

func BuildPlan(duration float64, cfg Config) (Plan, error) {
	if err := validate(duration, cfg); err != nil {
		return Plan{}, err
	}

	capacity := cfg.Columns * cfg.Rows
	preferredCount := math.Ceil(duration / cfg.IntervalSeconds)
	cellCount := capacity
	effectiveInterval := cfg.IntervalSeconds
	if preferredCount > float64(capacity) {
		effectiveInterval = duration / float64(capacity)
	} else {
		cellCount = int(preferredCount)
	}

	columns := min(cfg.Columns, cellCount)
	rows := int(math.Ceil(float64(cellCount) / float64(columns)))

	return Plan{
		StoryboardCellCount: cellCount,
		Columns:             columns,
		Rows:                rows,
		EffectiveInterval:   effectiveInterval,
	}, nil
}

func GenerateVTT(duration float64, cfg Config, plan Plan) (string, error) {
	if err := validate(duration, cfg); err != nil {
		return "", err
	}
	if err := validatePlan(cfg, plan); err != nil {
		return "", err
	}

	var content strings.Builder
	content.WriteString("WEBVTT\n\n")

	for index := 0; index < plan.StoryboardCellCount; index++ {
		start := float64(index) * plan.EffectiveInterval
		end := math.Min(float64(index+1)*plan.EffectiveInterval, duration)
		if start >= duration || end <= start {
			return "", fmt.Errorf("invalid cue range at storyboard cell %d", index)
		}

		column := index % plan.Columns
		row := index / plan.Columns
		x := column * cfg.Width
		y := row * cfg.Height

		fmt.Fprintf(&content, "%s --> %s\n", formatTimestamp(start), formatTimestamp(end))
		fmt.Fprintf(&content, "%s#xywh=%d,%d,%d,%d\n\n", StoryboardFilename, x, y, cfg.Width, cfg.Height)
	}

	return content.String(), nil
}

func ValidateAndWriteVTT(outputDir string, duration float64, cfg Config, plan Plan) (int64, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return 0, fmt.Errorf("read thumbnail directory: %w", err)
	}

	storyboardPath := filepath.Join(outputDir, StoryboardFilename)
	storyboardInfo, err := os.Lstat(storyboardPath)
	if err != nil {
		return 0, fmt.Errorf("inspect storyboard: %w", err)
	}
	if !storyboardInfo.Mode().IsRegular() || storyboardInfo.Size() == 0 {
		return 0, fmt.Errorf("storyboard must be a non-empty regular file")
	}

	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".jpg") && entry.Name() != StoryboardFilename {
			return 0, fmt.Errorf("unexpected additional storyboard image %q", entry.Name())
		}
	}

	content, err := GenerateVTT(duration, cfg, plan)
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(content, "WEBVTT\n\n") {
		return 0, fmt.Errorf("generated WebVTT has an invalid header")
	}

	vttPath := filepath.Join(outputDir, VTTFilename)
	if err := os.WriteFile(vttPath, []byte(content), 0o644); err != nil {
		return 0, fmt.Errorf("write WebVTT: %w", err)
	}

	return storyboardInfo.Size(), nil
}

func validate(duration float64, cfg Config) error {
	if duration <= 0 || duration > float64(math.MaxInt64)/1000 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return fmt.Errorf("duration must be a finite value greater than zero")
	}
	if cfg.IntervalSeconds <= 0 || math.IsNaN(cfg.IntervalSeconds) || math.IsInf(cfg.IntervalSeconds, 0) {
		return fmt.Errorf("thumbnail interval must be a finite value greater than zero")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Columns <= 0 || cfg.Rows <= 0 {
		return fmt.Errorf("thumbnail dimensions and grid values must be greater than zero")
	}

	return nil
}

func validatePlan(cfg Config, plan Plan) error {
	capacity := cfg.Columns * cfg.Rows
	if plan.StoryboardCellCount < 1 || plan.StoryboardCellCount > capacity {
		return fmt.Errorf("storyboard cell count must be between 1 and %d", capacity)
	}
	if plan.Columns < 1 || plan.Columns > cfg.Columns || plan.Rows < 1 || plan.Rows > cfg.Rows {
		return fmt.Errorf("storyboard plan exceeds configured grid")
	}
	if plan.Columns*plan.Rows < plan.StoryboardCellCount {
		return fmt.Errorf("storyboard plan grid cannot fit all cells")
	}
	if plan.EffectiveInterval <= 0 || math.IsNaN(plan.EffectiveInterval) || math.IsInf(plan.EffectiveInterval, 0) {
		return fmt.Errorf("effective interval must be a finite value greater than zero")
	}

	return nil
}

func formatTimestamp(seconds float64) string {
	totalMilliseconds := int64(math.Round(seconds * 1000))
	hours := totalMilliseconds / 3_600_000
	totalMilliseconds %= 3_600_000
	minutes := totalMilliseconds / 60_000
	totalMilliseconds %= 60_000
	secs := totalMilliseconds / 1_000
	milliseconds := totalMilliseconds % 1_000

	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, milliseconds)
}
