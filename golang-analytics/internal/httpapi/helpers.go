package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

func hasBearerToken(request *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	value := request.Header.Get("Authorization")
	prefix := "Bearer "
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return false
	}
	provided := strings.TrimSpace(value[len(prefix):])
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
