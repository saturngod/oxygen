package query

import (
	"context"
	"time"

	"github.com/google/uuid"
	"oxygen/analytics/internal/domain"
	"oxygen/analytics/internal/store"
)

// Service reads only the analytics database. It never reaches into the
// control-plane database, which keeps query load and retention independent.
type Service struct {
	store store.AnalyticsStore
	now   func() time.Time
}

func NewService(analyticsStore store.AnalyticsStore) *Service {
	return &Service{store: analyticsStore, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Build(ctx context.Context, organizationID, streamID uuid.UUID, period domain.Period) (domain.AnalyticsResponse, error) {
	now := s.now().UTC()
	start, end, granularity := period.Range(now)
	rows, err := s.store.QueryHourly(ctx, organizationID, streamID, start, end)
	if err != nil {
		return domain.AnalyticsResponse{}, err
	}
	coverageStart, err := s.store.CoverageStart(ctx)
	if err != nil {
		return domain.AnalyticsResponse{}, err
	}
	broadcasts, err := s.store.CountOverlappingSessions(ctx, organizationID, streamID, start, end)
	if err != nil {
		return domain.AnalyticsResponse{}, err
	}
	latestEventAt, err := s.store.LatestEventAt(ctx, organizationID, streamID)
	if err != nil {
		return domain.AnalyticsResponse{}, err
	}

	points := make([]domain.Point, period.PointCount())
	pointIndex := make(map[int64]int, len(points))
	for index := range points {
		bucket := period.BucketStart(addBucket(start, period, index))
		points[index] = domain.Point{Timestamp: bucket, Complete: !sameBucket(bucket, period.BucketStart(now), period)}
		if coverageStart == nil || bucket.Before(period.BucketStart(*coverageStart)) {
			continue
		}
		points[index].Value = intPointer(0)
		pointIndex[bucket.Unix()] = index
	}

	var summary domain.Summary
	for _, row := range rows {
		bucket := period.BucketStart(row.BucketStart)
		index, ok := pointIndex[bucket.Unix()]
		if !ok {
			continue
		}
		if row.PeakViewers > valueOrZero(points[index].Value) {
			points[index].Value = intPointer(row.PeakViewers)
		}
		if row.PeakViewers > summary.PeakViewers {
			summary.PeakViewers = row.PeakViewers
		}
		summary.ViewerIdentityAdditions += row.ViewerIdentityAdditions
		summary.PlaybackRequests += row.PlaylistRequests + row.SegmentRequests
	}
	summary.Broadcasts = broadcasts

	dataLagSeconds := int64(0)
	if latestEventAt != nil && now.After(*latestEventAt) {
		dataLagSeconds = int64(now.Sub(*latestEventAt).Seconds())
	}

	return domain.AnalyticsResponse{
		RangeStart:     start,
		RangeEnd:       end,
		CoverageStart:  coverageStart,
		Timezone:       "UTC",
		Granularity:    granularity,
		Points:         points,
		Summary:        summary,
		GeneratedAt:    now,
		DataLagSeconds: dataLagSeconds,
	}, nil
}

func (s *Service) Current(ctx context.Context, organizationID, streamID uuid.UUID) (*domain.SessionMetric, error) {
	return s.store.CurrentSession(ctx, organizationID, streamID)
}

func addBucket(start time.Time, period domain.Period, index int) time.Time {
	switch period {
	case domain.PeriodDay:
		return start.Add(time.Duration(index) * time.Hour)
	case domain.PeriodMonth:
		return start.AddDate(0, 0, index)
	case domain.PeriodYear:
		return start.AddDate(0, index, 0)
	default:
		return start
	}
}

func sameBucket(left, right time.Time, period domain.Period) bool {
	return period.BucketStart(left).Equal(period.BucketStart(right))
}

func intPointer(value int) *int {
	return &value
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
