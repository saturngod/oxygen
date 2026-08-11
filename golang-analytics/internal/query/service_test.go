package query

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"oxygen/analytics/internal/domain"
)

type fakeAnalyticsStore struct {
	rows          []domain.HourlyMetric
	coverage      *time.Time
	broadcasts    int64
	current       *domain.SessionMetric
	queriedOrg    uuid.UUID
	queriedStream uuid.UUID
}

func (f *fakeAnalyticsStore) QueryHourly(_ context.Context, organizationID, streamID uuid.UUID, _, _ time.Time) ([]domain.HourlyMetric, error) {
	f.queriedOrg = organizationID
	f.queriedStream = streamID
	return f.rows, nil
}

func (f *fakeAnalyticsStore) CountOverlappingSessions(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (int64, error) {
	return f.broadcasts, nil
}

func (f *fakeAnalyticsStore) CurrentSession(context.Context, uuid.UUID, uuid.UUID) (*domain.SessionMetric, error) {
	return f.current, nil
}

func (f *fakeAnalyticsStore) CoverageStart(context.Context) (*time.Time, error) {
	return f.coverage, nil
}

func (f *fakeAnalyticsStore) LatestEventAt(context.Context, uuid.UUID, uuid.UUID) (*time.Time, error) {
	return nil, nil
}

func TestBuildDayKeepsCoveredInactiveBucketsDistinctFromPreCoverage(t *testing.T) {
	organizationID := uuid.New()
	streamID := uuid.New()
	coverage := time.Date(2026, 8, 10, 10, 15, 0, 0, time.UTC)
	store := &fakeAnalyticsStore{
		coverage:   &coverage,
		broadcasts: 2,
		rows: []domain.HourlyMetric{
			{BucketStart: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC), PeakViewers: 7, ViewerIdentityAdditions: 3, PlaylistRequests: 4, SegmentRequests: 6},
		},
	}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 8, 10, 10, 30, 0, 0, time.UTC) }

	response, err := service.Build(context.Background(), organizationID, streamID, domain.PeriodDay)
	if err != nil {
		t.Fatal(err)
	}

	if len(response.Points) != 24 {
		t.Fatalf("expected 24 points, got %d", len(response.Points))
	}
	if response.Points[23].Value == nil || *response.Points[23].Value != 7 {
		t.Fatalf("expected covered 10:00 bucket to contain peak 7, got %#v", response.Points[20].Value)
	}
	if response.Points[0].Value != nil {
		t.Fatalf("expected pre-coverage point to be null, got %#v", response.Points[0].Value)
	}
	if response.Points[23].Complete {
		t.Fatal("expected current bucket to be incomplete")
	}
	if response.Summary.PlaybackRequests != 10 || response.Summary.Broadcasts != 2 {
		t.Fatalf("unexpected summary: %+v", response.Summary)
	}
	if store.queriedOrg != organizationID || store.queriedStream != streamID {
		t.Fatal("analytics query was not scoped to the requested tenant")
	}
}

func TestBuildMonthAndYearAggregateHourlyRows(t *testing.T) {
	store := &fakeAnalyticsStore{broadcasts: 1}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) }
	coverage := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	store.coverage = &coverage
	store.rows = []domain.HourlyMetric{
		{BucketStart: time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC), PeakViewers: 4, ViewerIdentityAdditions: 2, PlaylistRequests: 5, SegmentRequests: 7},
		{BucketStart: time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC), PeakViewers: 9, ViewerIdentityAdditions: 3, PlaylistRequests: 11, SegmentRequests: 13},
	}

	month, err := service.Build(context.Background(), uuid.New(), uuid.New(), domain.PeriodMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(month.Points) != 30 || month.Points[20].Value == nil || *month.Points[20].Value != 9 {
		t.Fatalf("unexpected month points: len=%d point=%#v", len(month.Points), month.Points[20].Value)
	}
	if month.Summary.ViewerIdentityAdditions != 5 || month.Summary.PlaybackRequests != 36 {
		t.Fatalf("unexpected month summary: %+v", month.Summary)
	}

	year, err := service.Build(context.Background(), uuid.New(), uuid.New(), domain.PeriodYear)
	if err != nil {
		t.Fatal(err)
	}
	if len(year.Points) != 12 || year.Points[11].Value == nil || *year.Points[11].Value != 9 {
		t.Fatalf("unexpected year points: len=%d point=%#v", len(year.Points), year.Points[11].Value)
	}
}
