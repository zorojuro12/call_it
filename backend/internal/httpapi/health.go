// Package httpapi wires REST handlers and the process's HTTP router.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// HealthHandler reports that the process is up. It deliberately checks
// nothing else in Phase 0 — no dependencies exist for it to check yet.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Headers are already sent, so a failed encode can't change the
	// response status — log it rather than silently dropping it.
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		slog.Error("healthz: failed to encode response", "error", err)
	}
}

// NewMux assembles the process's HTTP routes.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler)
	return mux
}
