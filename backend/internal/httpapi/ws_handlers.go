package httpapi

import (
	"net/http"

	"github.com/zorojuro12/call_it/backend/internal/ws"
)

// registerWSRoutes wires the room-scoped WebSocket socket. The nil
// MessageHandler is Phase 4b's seam — no gameplay handler exists yet
// in this phase.
func registerWSRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /api/v1/socket", ws.Handler(d.Hub, d.Issuer, ws.DefaultClientConfig(), nil))
}
