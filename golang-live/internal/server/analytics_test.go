package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"oxygen/live/internal/config"
)

func TestAnalyticsOutboxDeliversAndDeletesDurableBatch(t *testing.T) {
	var received AnalyticsEventBatch
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer analytics-token" {
			t.Fatal("expected analytics bearer token")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewAnalyticsClient(config.Config{AnalyticsURL: server.URL, AnalyticsToken: "analytics-token"})
	outbox := NewAnalyticsOutbox(root, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Enqueue(AnalyticsEventBatch{Events: []AnalyticsEvent{{EventID: "event-1", EventType: AnalyticsViewerSample, Sequence: 2}}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one durable entry, err=%v entries=%d", err, len(entries))
	}

	outbox.flush(context.Background())
	entries, err = os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(received.Events) != 1 {
		t.Fatalf("expected delivered entry to be removed: entries=%d batch=%+v", len(entries), received)
	}
}

func TestAnalyticsOutboxRetainsBatchOnServiceFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	root := t.TempDir()
	client := NewAnalyticsClient(config.Config{AnalyticsURL: server.URL, AnalyticsToken: "analytics-token"})
	outbox := NewAnalyticsOutbox(root, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := outbox.Enqueue(AnalyticsEventBatch{Events: []AnalyticsEvent{{EventID: "event-1", EventType: AnalyticsViewerSample, Sequence: 2, OccurredAt: time.Now().UTC()}}}); err != nil {
		t.Fatal(err)
	}
	outbox.flush(context.Background())
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected failed delivery to remain durable, got %d entries", len(entries))
	}
}

func TestFlushSnapshotsWritesAnalyticsEventAndSkipsLegacyRollup(t *testing.T) {
	var received AnalyticsEventBatch
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	root := t.TempDir()
	srv := New(config.Config{
		AnalyticsURL:        server.URL,
		AnalyticsToken:      "analytics-token",
		AnalyticsOutboxRoot: root,
		AnalyticsBatchSize:  10,
		ViewerTTL:           time.Minute,
		MaxTrackedViewers:   100,
		CallbackRoot:        t.TempDir(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	orgID := "00000000-0000-0000-0000-000000000011"
	streamID := "00000000-0000-0000-0000-000000000012"
	sessionID := "00000000-0000-0000-0000-000000000013"
	srv.tracker.StartSession("public-1", sessionID, SessionContext{OrganizationID: orgID, LiveStreamID: streamID})
	now := time.Now().UTC()
	srv.tracker.Observe("public-1", "viewer-1", "index.m3u8", now)

	srv.flushSnapshots(context.Background(), now)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one analytics outbox entry, err=%v entries=%d", err, len(entries))
	}
	srv.analyticsOutbox.flush(context.Background())

	if len(received.Events) != 1 {
		t.Fatalf("expected one analytics event, got %+v", received)
	}
	event := received.Events[0]
	if event.EventType != AnalyticsViewerSample || event.Sequence != 2 || event.CurrentViewers != 1 || event.IdentityAdditions != 1 {
		t.Fatalf("unexpected analytics event: %+v", event)
	}
}
