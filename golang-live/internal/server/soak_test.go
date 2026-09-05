package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"oxygen/live/internal/config"
)

func TestAdaptiveLiveStreamSoak(t *testing.T) {
	if os.Getenv("OXYGEN_LIVE_SOAK") != "1" {
		t.Skip("set OXYGEN_LIVE_SOAK=1 to run the real ffmpeg/RTMP/HLS soak test")
	}
	for _, adaptive := range []bool{true, false} {
		name := "source"
		if adaptive {
			name = "adaptive"
		}
		t.Run(name, func(t *testing.T) { runLiveStreamSoak(t, adaptive) })
	}
}

func runLiveStreamSoak(t *testing.T, adaptive bool) {

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	duration := 30 * time.Second
	if raw := os.Getenv("LIVE_SOAK_DURATION"); raw != "" {
		duration, err = time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("invalid LIVE_SOAK_DURATION: %v", err)
		}
	}

	if duration < 15*time.Second {
		t.Fatal("LIVE_SOAK_DURATION must be at least 15s for continuous playback checks")
	}
	started := make(chan struct{}, 1)
	ended := make(chan struct{}, 1)
	callbackRoot := t.TempDir()
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Live-Service-Token") != "service-secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/live/recover-active":
			_, _ = io.WriteString(w, `{"ok":true,"recovered":0}`)
		case "/internal/live/auth-publish":
			qualities := `[]`
			if adaptive {
				qualities = `["360p"]`
			}
			_, _ = fmt.Fprintf(w, `{"allowed":true,"stream":{"id":"00000000-0000-0000-0000-000000000012","organization_id":"00000000-0000-0000-0000-000000000011","public_id":"soak-stream","qualities":%s}}`, qualities)
		case "/internal/live/session-started":
			select {
			case started <- struct{}{}:
			default:
			}
			_, _ = io.WriteString(w, `{"ok":true,"session_id":"00000000-0000-0000-0000-000000000013"}`)
		case "/internal/live/session-ended":
			_, _ = io.WriteString(w, `{"ok":true}`)
			select {
			case ended <- struct{}{}:
			default:
			}
		case "/internal/live/session-failed":
			body, _ := io.ReadAll(r.Body)
			t.Errorf("live session failed during soak: %s", body)
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	rtmpAddr := availableTCPAddress(t)
	hlsRoot := t.TempDir()
	srv := New(config.Config{
		RTMPAddr:           rtmpAddr,
		HLSRoot:            hlsRoot,
		FFmpegBin:          ffmpeg,
		FFmpegVideoCodec:   "libx264",
		CallbackRoot:       callbackRoot,
		LaravelURL:         controlPlane.URL,
		ServiceToken:       "service-secret",
		ControlToken:       "control-secret",
		ViewerTTL:          45 * time.Second,
		RollupInterval:     15 * time.Second,
		MaxTrackedViewers:  100,
		MaxRTMPConnections: 10,
		MaxLiveTranscoders: 1,
		FFmpegWriteTimeout: 10 * time.Second,
		FFmpegStallTimeout: 30 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := srv.Prepare(); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Routes())
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.RunCallbacks(ctx)
	if !srv.RecoverActiveSessions(ctx) {
		t.Fatal("active-session recovery failed")
	}
	rtmpDone := make(chan error, 1)
	go func() { rtmpDone <- srv.RunRTMP(ctx) }()
	waitForCondition(t, 5*time.Second, func() bool { return srv.rtmpListening.Load() }, "RTMP listener")

	publishURL := fmt.Sprintf("rtmp://%s/live/soak-stream?key=publisher-secret", rtmpAddr)
	publisher := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error", "-re",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000",
		"-t", ffmpegDurationArgument(duration),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0",
		"-c:a", "aac", "-b:a", "96k", "-f", "flv", publishURL,
	)
	publisher.Stderr = os.Stderr
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	publisherDone := make(chan error, 1)
	go func() { publisherDone <- publisher.Wait() }()

	select {
	case <-started:
	case <-time.After(15 * time.Second):
		_ = publisher.Process.Kill()
		t.Fatal("session did not start")
	}
	playbackURL := httpServer.URL + "/live/soak-stream/index.m3u8"
	client := &http.Client{Timeout: 3 * time.Second}
	waitForCondition(t, 20*time.Second, func() bool {
		response, err := client.Get(playbackURL)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode == http.StatusOK
	}, "HTTP HLS playlist")
	decoderCtx, stopDecoder := context.WithCancel(ctx)
	defer stopDecoder()
	decoder := exec.CommandContext(decoderCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-xerror", "-progress", "pipe:1", "-i", playbackURL, "-f", "null", "-")
	progress, err := decoder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	decoder.Stderr = os.Stderr
	if err := decoder.Start(); err != nil {
		t.Fatal(err)
	}
	progressValues := make(chan int64, 1)
	decoderDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(progress)
		for scanner.Scan() {
			if raw, ok := strings.CutPrefix(scanner.Text(), "out_time_us="); ok {
				if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
					select {
					case progressValues <- value:
					default:
					}
				}
			}
		}
		decoderDone <- decoder.Wait()
	}()
	defer func() { stopDecoder(); <-decoderDone }()
	lastProgress := time.Now()
	var lastPTS int64
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
monitor:
	for {
		select {
		case err := <-publisherDone:
			if err != nil {
				t.Fatalf("publisher failed: %v", err)
			}
			if lastPTS == 0 {
				t.Fatal("no decoded playback progress")
			}
			break monitor
		case pts := <-progressValues:
			if pts > lastPTS {
				lastPTS = pts
				lastProgress = time.Now()
			}
		case <-ticker.C:
			if time.Since(lastProgress) > 20*time.Second {
				t.Fatal("HTTP playback stopped progressing")
			}
			var size int64
			_ = filepath.Walk(hlsRoot, func(_ string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					size += info.Size()
				}
				return nil
			})
			if size > 128<<20 {
				t.Fatal("HLS storage exceeded 128 MiB")
			}
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			if memory.HeapAlloc > baseline.HeapAlloc+128<<20 {
				t.Fatal("Go heap grew by more than 128 MiB")
			}
		}
	}
	select {
	case <-ended:
	case <-time.After(15 * time.Second):
		t.Fatal("terminal session callback was not delivered")
	}
	waitForCondition(t, 5*time.Second, func() bool {
		entries, err := os.ReadDir(callbackRoot)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				return false
			}
		}
		return true
	}, "terminal callback acknowledgement and outbox cleanup")

	cancel()
	select {
	case err := <-rtmpDone:
		if err != nil {
			t.Fatalf("RTMP server shutdown failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RTMP server did not stop")
	}
}

func TestFFmpegDurationArgumentUsesSeconds(t *testing.T) {
	if got := ffmpegDurationArgument(24 * time.Hour); got != "86400" {
		t.Fatalf("expected ffmpeg-compatible 24-hour duration, got %q", got)
	}
	if got := ffmpegDurationArgument(1500 * time.Millisecond); got != "1.5" {
		t.Fatalf("expected fractional seconds, got %q", got)
	}
}

func ffmpegDurationArgument(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}

func availableTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	return address
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}
