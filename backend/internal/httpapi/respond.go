package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteData wraps v in the success envelope {"data": ...} and writes it
// with the given status.
func WriteData(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The header is already sent, so a failed encode can't change the
	// response status — log it rather than silently dropping it.
	if err := json.NewEncoder(w).Encode(struct {
		Data any `json:"data"`
	}{v}); err != nil {
		slog.Error("httpapi: failed to encode response", "error", err)
	}
}
