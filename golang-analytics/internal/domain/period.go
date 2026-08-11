package domain

import (
	"fmt"
	"time"
)

type Period string

const (
	PeriodDay   Period = "day"
	PeriodMonth Period = "month"
	PeriodYear  Period = "year"
)

func ParsePeriod(value string) (Period, error) {
	period := Period(value)
	switch period {
	case PeriodDay, PeriodMonth, PeriodYear:
		return period, nil
	default:
		return "", fmt.Errorf("period must be day, month, or year")
	}
}

func (p Period) Range(end time.Time) (time.Time, time.Time, string) {
	end = end.UTC()
	switch p {
	case PeriodDay:
		start := end.Add(-23 * time.Hour).Truncate(time.Hour)
		return start, end, "hour"
	case PeriodMonth:
		start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
		return start, end, "day"
	case PeriodYear:
		start := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
		return start, end, "month"
	default:
		panic("invalid analytics period")
	}
}

func (p Period) BucketStart(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	switch p {
	case PeriodDay:
		return timestamp.Truncate(time.Hour)
	case PeriodMonth:
		return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, time.UTC)
	case PeriodYear:
		return time.Date(timestamp.Year(), timestamp.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		panic("invalid analytics period")
	}
}

func (p Period) PointCount() int {
	switch p {
	case PeriodDay:
		return 24
	case PeriodMonth:
		return 30
	case PeriodYear:
		return 12
	default:
		return 0
	}
}
