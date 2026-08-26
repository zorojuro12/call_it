// Package httpapi wires REST handlers and the process's HTTP router.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
	"github.com/zorojuro12/call_it/backend/internal/round"
	"github.com/zorojuro12/call_it/backend/internal/wager"
	"github.com/zorojuro12/call_it/backend/internal/ws"
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

// Deps are the constructed dependencies every handler needs. cmd/api
// builds these once at startup and passes them to NewMux.
type Deps struct {
	Accounts *account.Service
	Rooms    *room.Service
	Rounds   *round.Service
	Wagers   *wager.Service
	Store    *redisstore.Store
	Issuer   *auth.Issuer
	Hub      *ws.Hub
}

// NewMux assembles the process's HTTP routes.
func NewMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler)
	registerAuthRoutes(mux, d)
	registerRoomRoutes(mux, d)
	registerAccountRoutes(mux, d)
	registerWSRoutes(mux, d)
	return mux
}
