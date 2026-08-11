package maintenance

import (
	"context"
	"log/slog"
	"time"

	"oxygen/analytics/internal/store"
)

type Scheduler struct {
	store           store.MaintenanceStore
	retentionDays   int
	reconciliationH int
	logger          *slog.Logger
	now             func() time.Time
}

func NewScheduler(maintenanceStore store.MaintenanceStore, retentionDays, reconciliationHours int, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		store:           maintenanceStore,
		retentionDays:   retentionDays,
		reconciliationH: reconciliationHours,
		logger:          logger,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	if s.store == nil {
		return
	}
	reconcileTicker := time.NewTicker(time.Hour)
	pruneTicker := time.NewTicker(24 * time.Hour)
	defer reconcileTicker.Stop()
	defer pruneTicker.Stop()

	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileTicker.C:
			s.reconcile(ctx)
		case <-pruneTicker.C:
			s.prune(ctx)
		}
	}
}

func (s *Scheduler) reconcile(ctx context.Context) {
	now := s.now().UTC().Truncate(time.Hour)
	hours := s.reconciliationH
	if hours <= 0 {
		hours = 48
	}
	if err := s.store.Reconcile(ctx, now.Add(-time.Duration(hours)*time.Hour), now); err != nil {
		s.logger.Error("analytics reconciliation failed", "error", err)
	}
}

func (s *Scheduler) prune(ctx context.Context) {
	days := s.retentionDays
	if days <= 0 {
		days = 30
	}
	deleted, err := s.store.PruneEvents(ctx, s.now().UTC().AddDate(0, 0, -days))
	if err != nil {
		s.logger.Error("analytics event pruning failed", "error", err)
		return
	}
	s.logger.Info("analytics events pruned", "deleted", deleted)
}
