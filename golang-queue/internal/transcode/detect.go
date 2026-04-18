package transcode

import (
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
)

// DetectVideoCodec returns the best available ffmpeg video encoder.
// Priority: Apple VideoToolbox (macOS) → NVIDIA NVENC → libx264 fallback.
func DetectVideoCodec(ffmpegBin string) string {
	encoders := listEncoders(ffmpegBin)

	if runtime.GOOS == "darwin" {
		if strings.Contains(encoders, "h264_videotoolbox") {
			slog.Info("gpu detected: Apple VideoToolbox", "codec", "h264_videotoolbox")
			return "h264_videotoolbox"
		}
	}

	if strings.Contains(encoders, "h264_nvenc") && nvidiaPresent() {
		slog.Info("gpu detected: NVIDIA NVENC", "codec", "h264_nvenc")
		return "h264_nvenc"
	}

	slog.Info("no hardware encoder found, using software", "codec", "libx264")
	return "libx264"
}

func listEncoders(ffmpegBin string) string {
	out, err := exec.Command(ffmpegBin, "-encoders", "-v", "quiet").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func nvidiaPresent() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}
