package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestClockConversionAcrossMultipleDays(t *testing.T) {
	for _, seconds := range []int64{102481, 102482, 29 * 3600, 7 * 24 * 3600, 60 * 24 * 3600} {
		for _, rate := range []int{90000, 48000, 44100} {
			pts := time.Duration(seconds)*time.Second + 500*time.Millisecond
			want := seconds*int64(rate) + int64(rate)/2
			if got := toClock(pts, rate); got != want {
				t.Fatalf("pts=%s rate=%d: got %d want %d", pts, rate, got, want)
			}
		}
	}
}

func TestViewerExpiryRefreshAndCapacity(t *testing.T) {
	tracker := NewTracker(time.Second, 2)
	tracker.StartSession("stream", "session")
	now := time.Now()
	tracker.Observe("stream", "a", "index.m3u8", now)
	tracker.Observe("stream", "b", "index.m3u8", now.Add(500*time.Millisecond))
	tracker.Observe("stream", "a", "index.m3u8", now.Add(900*time.Millisecond))
	tracker.Observe("stream", "c", "index.m3u8", now.Add(1600*time.Millisecond))
	snapshots := tracker.Snapshots(now.Add(1600 * time.Millisecond))
	if snapshots[0].CurrentViewers != 2 {
		t.Fatalf("unexpected viewers: %+v", snapshots)
	}
	m := tracker.streams["stream"]
	if _, exists := m.Viewers["b"]; exists {
		t.Fatal("expired viewer retained")
	}
	if _, exists := m.Viewers["c"]; !exists {
		t.Fatal("expired slot was not reused")
	}
	if m.viewerOrder.Len() != 2 || len(m.viewerElements) != 2 {
		t.Fatal("expiry index grew beyond capacity")
	}
	if got := tracker.Snapshots(now.Add(3 * time.Second))[0].CurrentViewers; got != 0 {
		t.Fatalf("got %d stale viewers", got)
	}
}

func TestCallbackPrepareRejectsReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0700)
	outbox := NewCallbackOutbox(root, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Prepare(); err == nil {
		t.Fatal("read-only callback storage accepted")
	}
}

func TestAnalyticsCapacityIncludesDeadLetters(t *testing.T) {
	root := t.TempDir()
	dead := filepath.Join(root, "dead-letter")
	if err := os.Mkdir(dead, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dead, "entry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := checkAnalyticsCapacity(root, 1); err == nil {
		t.Fatal("accepted telemetry beyond storage limit")
	}
}

func BenchmarkViewerObservation(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			tracker := NewTracker(time.Minute, count)
			tracker.StartSession("stream", "session")
			now := time.Now()
			for i := 0; i < count; i++ {
				tracker.Observe("stream", strconv.Itoa(i), "segment.m4s", now)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tracker.Observe("stream", "0", "segment.m4s", now)
			}
		})
	}
}
