package httpapi

import (
	"net/http"

	"github.com/google/uuid"
	"oxygen/analytics/internal/domain"
	"oxygen/analytics/internal/query"
	"oxygen/analytics/internal/store"
)

type AnalyticsHandler struct {
	service *query.Service
	purger  store.PurgeStore
	token   string
}

func (h AnalyticsHandler) Purge(writer http.ResponseWriter, request *http.Request) {
	if !hasBearerToken(request, h.token) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.purger == nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics purge is unavailable")
		return
	}
	organizationID, streamID, ok := parseEntityIDs(writer, request)
	if !ok {
		return
	}
	if err := h.purger.PurgeStream(request.Context(), organizationID, streamID); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics purge failed")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h AnalyticsHandler) Handle(writer http.ResponseWriter, request *http.Request) {
	if !hasBearerToken(request, h.token) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.service == nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics query is unavailable")
		return
	}
	organizationID, streamID, ok := parseEntityIDs(writer, request)
	if !ok {
		return
	}
	period, err := domain.ParsePeriod(request.URL.Query().Get("period"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	response, err := h.service.Build(request.Context(), organizationID, streamID, period)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics query failed")
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (h AnalyticsHandler) Live(writer http.ResponseWriter, request *http.Request) {
	if !hasBearerToken(request, h.token) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.service == nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics query is unavailable")
		return
	}
	organizationID, streamID, ok := parseEntityIDs(writer, request)
	if !ok {
		return
	}
	metric, err := h.service.Current(request.Context(), organizationID, streamID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "analytics query failed")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"session": metric})
}

func parseEntityIDs(writer http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	organizationID, err := uuid.Parse(request.PathValue("organizationID"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "organizationID must be a UUID")
		return uuid.Nil, uuid.Nil, false
	}
	streamID, err := uuid.Parse(request.PathValue("streamID"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "streamID must be a UUID")
		return uuid.Nil, uuid.Nil, false
	}
	return organizationID, streamID, true
}
