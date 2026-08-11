package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"oxygen/analytics/internal/domain"
	"oxygen/analytics/internal/store"
)

type IngestHandler struct {
	events   store.EventStore
	token    string
	maxBatch int
	maxBody  int64
	maxAge   time.Duration
}

func (h IngestHandler) Handle(writer http.ResponseWriter, request *http.Request) {
	if !hasBearerToken(request, h.token) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.events == nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics ingestion is unavailable")
		return
	}
	if h.maxBody <= 0 {
		h.maxBody = 2 * 1024 * 1024
	}
	request.Body = http.MaxBytesReader(writer, request.Body, h.maxBody)
	var batch domain.EventBatch
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		if err == io.EOF {
			writeError(writer, http.StatusBadRequest, "request body is required")
			return
		}
		if strings.Contains(err.Error(), "request body too large") || strings.Contains(err.Error(), "http: request body too large") {
			writeError(writer, http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		writeError(writer, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	if len(batch.Events) == 0 || (h.maxBatch > 0 && len(batch.Events) > h.maxBatch) {
		writeError(writer, http.StatusRequestEntityTooLarge, "event batch exceeds the configured limit")
		return
	}
	now := time.Now().UTC()
	for _, event := range batch.Events {
		if err := event.Validate(h.maxAge, now); err != nil {
			writeError(writer, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	result, err := h.events.IngestBatch(request.Context(), batch.Events)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics ingestion failed")
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}
