package httpapi

import (
	"context"
	"net/http"
)

type HealthHandler struct {
	ping func(context.Context) error
}

func (h HealthHandler) Health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h HealthHandler) Ready(writer http.ResponseWriter, request *http.Request) {
	if h.ping == nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics database is not configured")
		return
	}
	if err := h.ping(request.Context()); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics database is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}
