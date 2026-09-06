package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"oxygen/analytics/internal/config"
	"oxygen/analytics/internal/domain"
	"oxygen/analytics/internal/query"
)

type fakeEventStore struct {
	events               []domain.Event
	purgedOrganizationID uuid.UUID
	purgedStreamID       uuid.UUID
}

func (f *fakeEventStore) IngestBatch(_ context.Context, events []domain.Event) (domain.IngestResult, error) {
	f.events = append(f.events, events...)
	return domain.IngestResult{Accepted: len(events)}, nil
}

func (f *fakeEventStore) PurgeStream(_ context.Context, organizationID, streamID uuid.UUID) error {
	f.purgedOrganizationID = organizationID
	f.purgedStreamID = streamID
	return nil
}

type fakeQueryStore struct{}

func (fakeQueryStore) QueryHourly(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]domain.HourlyMetric, error) {
	return nil, nil
}
func (fakeQueryStore) CountOverlappingSessions(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (int64, error) {
	return 0, nil
}
func (fakeQueryStore) CurrentSession(context.Context, uuid.UUID, uuid.UUID) (*domain.SessionMetric, error) {
	return nil, nil
}
func (fakeQueryStore) CoverageStart(context.Context) (*time.Time, error) { return nil, nil }
func (fakeQueryStore) LatestEventAt(context.Context, uuid.UUID, uuid.UUID) (*time.Time, error) {
	return nil, nil
}

func testRouter(events *fakeEventStore) http.Handler {
	cfg := config.Config{IngestToken: "ingest-token", QueryToken: "query-token", MaximumBatchSize: 5, MaximumRequestBodyBytes: 2048}
	return NewRouter(RouterDependencies{Config: cfg, Events: events, Analytics: query.NewService(fakeQueryStore{}), Purger: events, Ping: func(context.Context) error { return nil }})
}

func TestPurgeRequiresBearerTokenAndDeletesStreamAnalytics(t *testing.T) {
	events := &fakeEventStore{}
	router := testRouter(events)
	organizationID := uuid.New()
	streamID := uuid.New()
	path := "/internal/v1/organizations/" + organizationID.String() + "/streams/" + streamID.String()

	request := httptest.NewRequest(http.MethodDelete, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, path, nil)
	request.Header.Set("Authorization", "Bearer query-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected no content, got %d", response.Code)
	}
	if events.purgedOrganizationID != organizationID || events.purgedStreamID != streamID {
		t.Fatalf("unexpected purge target: organization=%s stream=%s", events.purgedOrganizationID, events.purgedStreamID)
	}
}

func TestIngestRequiresBearerTokenAndAcceptsBatch(t *testing.T) {
	events := &fakeEventStore{}
	router := testRouter(events)
	event := domain.Event{EventID: uuid.New(), EventType: domain.EventSessionStarted, SchemaVersion: 1, Sequence: 1, OccurredAt: time.Now().UTC(), OrganizationID: uuid.New(), LiveStreamID: uuid.New(), SessionID: uuid.New()}
	body, err := json.Marshal(domain.EventBatch{Events: []domain.Event{event}})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/events/batch", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/internal/v1/events/batch", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer ingest-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || len(events.events) != 1 {
		t.Fatalf("expected accepted batch, got status=%d events=%d", response.Code, len(events.events))
	}
}

func TestQueryAndHealthEndpointsAuthenticate(t *testing.T) {
	router := testRouter(&fakeEventStore{})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected health 200, got %d", response.Code)
	}

	organizationID := uuid.New()
	streamID := uuid.New()
	request = httptest.NewRequest(http.MethodGet, "/internal/v1/organizations/"+organizationID.String()+"/streams/"+streamID.String()+"/analytics?period=year", nil)
	request.Header.Set("Authorization", "Bearer query-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected analytics 200, got %d", response.Code)
	}
	var payload domain.AnalyticsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Points) != 12 {
		t.Fatalf("expected 12 year points, got %d", len(payload.Points))
	}
}

func TestIngestRejectsMalformedAndOversizedBatches(t *testing.T) {
	router := testRouter(&fakeEventStore{})
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/events/batch", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer ingest-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed body 400, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/internal/v1/events/batch", strings.NewReader(`{"events":[{},{},{},{},{},{}]}`))
	request.Header.Set("Authorization", "Bearer ingest-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized batch 413, got %d", response.Code)
	}
}
