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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"

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
	authenticated := make(chan time.Time, 1)
	callbackRoot := t.TempDir()
	var playbackURL string
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
				qualities = `["720p","480p"]`
			}
			select {
			case authenticated <- time.Now():
			default:
			}
			_, _ = fmt.Fprintf(w, `{"allowed":true,"stream":{"id":"00000000-0000-0000-0000-000000000012","organization_id":"00000000-0000-0000-0000-000000000011","public_id":"soak-stream","qualities":%s}}`, qualities)
		case "/internal/live/session-started":
			select {
			case authenticatedAt := <-authenticated:
				if elapsed := time.Since(authenticatedAt); elapsed > 8*time.Second {
					t.Errorf("HLS readiness took %s after authentication; target is 8s", elapsed)
				}
			default:
				t.Error("session started without a recorded authentication time")
			}
			response, err := (&http.Client{Timeout: 3 * time.Second}).Get(playbackURL)
			if err != nil {
				t.Errorf("HLS was not reachable when the session became live: %v", err)
			} else {
				_ = response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Errorf("HLS returned %d when the session became live", response.StatusCode)
				}
			}
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
	playbackURL = httpServer.URL + "/live/soak-stream/index.m3u8"

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
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30000/1001",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000",
		"-t", ffmpegDurationArgument(duration),
		"-c:v", "libx264", "-profile:v", "baseline", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-bf", "0",
		"-c:a", "aac", "-b:a", "96k", "-output_ts_offset", "60", "-f", "flv", publishURL,
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
	originStartedAt := time.Now()
	fetchHLSFile(t, client, mustParseURL(t, playbackURL))
	if elapsed := time.Since(originStartedAt); elapsed > 500*time.Millisecond {
		t.Errorf("ready origin playlist took %s; target is 500ms", elapsed)
	}
	validateHLSOverHTTP(t, client, playbackURL)
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
	select {
	case lastPTS = <-progressValues:
		lastProgress = time.Now()
	case <-time.After(5 * time.Second):
		t.Fatal("first playable media exceeded the 5 second target")
	}
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

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}

func validateHLSOverHTTP(t *testing.T, client *http.Client, masterURL string) {
	t.Helper()
	masterBase, err := url.Parse(masterURL)
	if err != nil {
		t.Fatal(err)
	}
	masterBytes := fetchHLSFile(t, client, masterBase)
	parsed, err := playlist.Unmarshal(masterBytes)
	if err != nil {
		t.Fatalf("parse served master playlist: %v", err)
	}
	master, ok := parsed.(*playlist.Multivariant)
	if !ok {
		t.Fatal("served master is not a multivariant playlist")
	}
	references := make(map[string]struct{})
	for _, variant := range master.Variants {
		references[variant.URI] = struct{}{}
	}
	for _, rendition := range master.Renditions {
		if rendition.URI != nil {
			references[*rendition.URI] = struct{}{}
		}
	}
	for reference := range references {
		mediaURL, err := masterBase.Parse(reference)
		if err != nil {
			t.Fatal(err)
		}
		mediaBytes := fetchHLSFile(t, client, mediaURL)
		parsedMedia, err := playlist.Unmarshal(mediaBytes)
		if err != nil {
			t.Fatalf("parse served media playlist %s: %v", reference, err)
		}
		media, ok := parsedMedia.(*playlist.Media)
		if !ok || media.Map == nil || len(media.Segments) == 0 {
			t.Fatalf("served media playlist %s is incomplete", reference)
		}
		initURL, err := mediaURL.Parse(media.Map.URI)
		if err != nil {
			t.Fatal(err)
		}
		fetchHLSFile(t, client, initURL)
		for _, segment := range media.Segments {
			segmentURL, err := mediaURL.Parse(segment.URI)
			if err != nil {
				t.Fatal(err)
			}
			fetchHLSFile(t, client, segmentURL)
		}
	}
}

func fetchHLSFile(t *testing.T, client *http.Client, target *url.URL) []byte {
	t.Helper()
	response, err := client.Get(target.String())
	if err != nil {
		t.Fatalf("fetch HLS URL %s: %v", target, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HLS URL %s returned %d", target, response.StatusCode)
	}
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatalf("HLS URL %s returned an empty file", target)
	}

	return contents
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
