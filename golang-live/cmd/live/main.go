package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"oxygen/live/internal/config"
	"oxygen/live/internal/server"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv := server.New(cfg, log)
	if err := srv.Prepare(); err != nil {
		log.Error("live service preparation failed", "err", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go srv.RunRollups(ctx)
	go srv.RunCallbacks(ctx)
	go srv.RunAnalyticsOutbox(ctx)

	rtmpDone := make(chan struct{})
	go func() {
		defer close(rtmpDone)

		if !srv.RecoverActiveSessions(ctx) {
			return
		}

		if err := srv.RunRTMP(ctx); err != nil {
			log.Error("rtmp server failed", "err", err)
			stop()
		}
	}()

	go func() {
		log.Info("live service listening", "addr", cfg.Addr, "rtmp_addr", cfg.RTMPAddr, "hls_root", cfg.HLSRoot)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown failed", "err", err)
	}

	select {
	case <-rtmpDone:
	case <-shutdownCtx.Done():
		log.Error("rtmp listener shutdown timed out", "err", shutdownCtx.Err())
		return
	}

	rtmpConnectionsDone := make(chan struct{})
	go func() {
		srv.WaitForRTMPConnections()
		close(rtmpConnectionsDone)
	}()

	select {
	case <-rtmpConnectionsDone:
	case <-shutdownCtx.Done():
		log.Error("rtmp publisher drain timed out", "err", shutdownCtx.Err())
	}
}
