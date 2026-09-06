package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"oxygen/analytics/internal/config"
	"oxygen/analytics/internal/httpapi"
	"oxygen/analytics/internal/maintenance"
	"oxygen/analytics/internal/query"
	"oxygen/analytics/internal/store/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("analytics service stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "migrate":
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return postgres.RunMigrations(cfg.DatabaseURL, migrationsPath())
		case "reconcile":
			return runReconcile(args[1:])
		case "serve":
			// Explicit serve is accepted for container entrypoints.
		default:
			return fmt.Errorf("unknown analytics command %q", args[0])
		}
	}
	return runServer()
}

func runServer() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := postgres.RunMigrations(cfg.DatabaseURL, migrationsPath()); err != nil {
		return err
	}
	analyticsStore, err := postgres.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer analyticsStore.Close()

	logger := slog.Default()
	queryService := query.NewService(analyticsStore)
	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      httpapi.NewRouter(httpapi.RouterDependencies{Config: cfg, Events: analyticsStore, Analytics: queryService, Purger: analyticsStore, Ping: analyticsStore.Ping}),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	scheduler := maintenance.NewScheduler(analyticsStore, cfg.RawRetentionDays, cfg.ReconciliationHours, logger)
	go scheduler.Run(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("analytics API listening", "addr", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("analytics HTTP server: %w", err)
	}
	return nil
}

func runReconcile(args []string) error {
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	hours := flags.Int("hours", 48, "number of hours to reconcile")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	analyticsStore, err := postgres.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer analyticsStore.Close()
	now := time.Now().UTC().Truncate(time.Hour)
	return analyticsStore.Reconcile(ctx, now.Add(-time.Duration(*hours)*time.Hour), now)
}

func migrationsPath() string {
	if value := os.Getenv("ANALYTICS_MIGRATIONS_PATH"); value != "" {
		return value
	}
	if _, err := os.Stat("migrations"); err == nil {
		return "migrations"
	}
	return filepath.Join("/app", "migrations")
}
