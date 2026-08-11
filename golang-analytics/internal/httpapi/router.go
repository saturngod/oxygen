package httpapi

import (
	"context"
	"net/http"
	"time"

	"oxygen/analytics/internal/config"
	"oxygen/analytics/internal/query"
	"oxygen/analytics/internal/store"
)

type RouterDependencies struct {
	Config    config.Config
	Events    store.EventStore
	Analytics *query.Service
	Ping      func(context.Context) error
}

func NewRouter(dependencies RouterDependencies) http.Handler {
	mux := http.NewServeMux()
	ingest := IngestHandler{events: dependencies.Events, token: dependencies.Config.IngestToken, maxBatch: dependencies.Config.MaximumBatchSize, maxBody: dependencies.Config.MaximumRequestBodyBytes, maxAge: time.Duration(dependencies.Config.RawRetentionDays) * 24 * time.Hour}
	analytics := AnalyticsHandler{service: dependencies.Analytics, token: dependencies.Config.QueryToken}
	health := HealthHandler{ping: dependencies.Ping}

	mux.HandleFunc("GET /healthz", health.Health)
	mux.HandleFunc("GET /readyz", health.Ready)
	mux.HandleFunc("POST /internal/v1/events/batch", ingest.Handle)
	mux.HandleFunc("GET /internal/v1/organizations/{organizationID}/streams/{streamID}/analytics", analytics.Handle)
	mux.HandleFunc("GET /internal/v1/organizations/{organizationID}/streams/{streamID}/live", analytics.Live)

	return recoverPanic(mux)
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				writeError(writer, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}
