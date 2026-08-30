package httpapi

import (
	"net/http"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/ws"
)

const (
	wsConnectLimit  = 30
	wsConnectWindow = time.Minute
)

// registerWSRoutes wires the room-scoped WebSocket socket to the real
// message router — d.Rounds and d.Wagers replace Phase 4a's nil
// MessageHandler seam. d.Rounds also ends a departing client's session
// on disconnect (its EndSession satisfies ws.SessionEnder).
func registerWSRoutes(mux *http.ServeMux, d Deps) {
	router := ws.NewRouter(d.Rounds, d.Wagers)

	// A nil *round.Service assigned directly to the ws.SessionEnder
	// parameter would not compare equal to a nil interface (the classic
	// typed-nil-in-interface gotcha) — Handler's own "sessions != nil"
	// check would then pass and call EndSession on a nil receiver.
	// Building the interface value only when d.Rounds is actually set
	// keeps it a true nil for callers (tests) that construct Deps
	// without a round service.
	var sessions ws.SessionEnder
	if d.Rounds != nil {
		sessions = d.Rounds
	}

	var opts []ws.HandlerOption
	if len(d.AllowedOrigins) > 0 {
		opts = append(opts, ws.WithAllowedOrigins(d.AllowedOrigins))
	}

	mux.Handle("GET /api/v1/socket", wsThrottle(d.Store, d.Issuer)(ws.Handler(d.Hub, d.Issuer, ws.DefaultClientConfig(), router.Handle, sessions, opts...)))
}

// wsThrottle limits WebSocket connection attempts through the shared
// rate limiter (CLAUDE.md: one sliding-window limiter, every call
// site) — the socket route had no throttle at all until this fix,
// letting a single valid token open unbounded connections, each
// holding two goroutines and a send buffer for the life of the
// process.
func wsThrottle(store *redisstore.Store, issuer *auth.Issuer) func(http.Handler) http.Handler {
	return RateLimit(store, LimitPolicy{
		Scope:  "ws_connect",
		Limit:  wsConnectLimit,
		Window: wsConnectWindow,
		KeyFn:  wsThrottleKey(issuer),
	})
}

// wsThrottleKey keys a verifiable token by user ID, matching
// apiThrottle's per-user quota; anything else — missing, garbage, or
// expired — falls back to the caller's IP, matching authThrottle. It
// cannot simply reuse apiThrottle: that reads claims already placed in
// the request context by RequireAuth, which only checks the
// Authorization header — the socket route's own auth accepts a
// ?token= query parameter too (browsers cannot set headers on a
// WebSocket handshake), so this keys off the same extraction
// ws.Handler itself uses.
func wsThrottleKey(issuer *auth.Issuer) func(*http.Request) string {
	return func(r *http.Request) string {
		if token := ws.ExtractToken(r); token != "" {
			if claims, err := issuer.Verify(token); err == nil && claims.UserID != "" {
				return "user:" + claims.UserID
			}
		}
		return "ip:" + ClientIP(r)
	}
}
