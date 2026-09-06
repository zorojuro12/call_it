package ws

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/auth"
)

// handlerOpts collects Handler's optional configuration, built from the
// HandlerOption values passed to it.
type handlerOpts struct {
	allowedOrigins map[string]bool
}

// HandlerOption configures optional Handler behavior via the functional
// options pattern (.claude/rules/ecc/golang/patterns.md), so existing
// call sites that pass none keep compiling unchanged.
type HandlerOption func(*handlerOpts)

// WithAllowedOrigins restricts the WebSocket upgrade to browser requests
// whose Origin header exactly matches one of origins. A request with no
// Origin header (a non-browser client, such as cmd/callit-cli) is always
// allowed — browsers always send Origin on a WebSocket handshake, so its
// absence is not a bypass.
func WithAllowedOrigins(origins []string) HandlerOption {
	return func(o *handlerOpts) {
		allowed := make(map[string]bool, len(origins))
		for _, origin := range origins {
			allowed[origin] = true
		}
		o.allowedOrigins = allowed
	}
}

// Sessions resumes a room member's pending session end on connect,
// schedules one on disconnect, and reports a room member's current
// balance — *round.Service satisfies this. Declared here (not imported
// as a concrete type) so Handler does not need a second entry point for
// tests that don't care about it.
type Sessions interface {
	ResumeSession(roomID, userID string)
	ScheduleEndSession(roomID, userID string, guest bool)
	Balance(roomID, userID string) (int64, error)
}

// Handler builds an http.HandlerFunc that authenticates a room-scoped
// JWT, upgrades the connection, and wires the resulting client into
// hub. onMessage is the seam Phase 4b fills for gameplay; a nil value
// makes every inbound message an unknown-type error reply. sessions
// resumes a pending session end on connect and schedules one on
// disconnect; a nil value skips both steps (every existing 4a test that
// doesn't care about it passes nil).
func Handler(hub *Hub, issuer *auth.Issuer, cfg ClientConfig, onMessage MessageHandler, sessions Sessions, opts ...HandlerOption) http.HandlerFunc {
	var o handlerOpts
	for _, opt := range opts {
		opt(&o)
	}

	upgrader := websocket.Upgrader{}
	if o.allowedOrigins != nil {
		upgrader.CheckOrigin = func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return o.allowedOrigins[origin]
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		token := ExtractToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := issuer.Verify(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if claims.RoomID == "" {
			http.Error(w, "token is not scoped to a room", http.StatusForbidden)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		if sessions != nil {
			sessions.ResumeSession(claims.RoomID, claims.UserID)
		}

		ident := Identity{UserID: claims.UserID, DisplayName: claims.DisplayName, Guest: claims.Guest}
		c := NewClient(conn, ident, cfg)
		c.RoomID = claims.RoomID
		room := hub.Join(claims.RoomID, c)

		// Balance is looked up only after hub.Join, deliberately: it is a
		// real Redis round trip, and this client must already be a room
		// member before anything that takes real I/O runs — otherwise the
		// window between this connection's handshake completing and its
		// hub.Join actually landing widens enough for it to miss a
		// broadcast another member's action sends in between.
		// ResumeSession above has no such cost (mutex-only, no I/O), which
		// is why it alone runs ahead of hub.Join.
		var balance int64
		if sessions != nil {
			balance, err = sessions.Balance(claims.RoomID, claims.UserID)
			if err != nil {
				// Best-effort: the connection itself is already real by
				// this point, and a display-only balance is not worth
				// failing it over. JoinRoom always runs (via the REST
				// join call) before a client ever holds a room token to
				// connect with, so this should not happen in practice.
				log.Printf("ws: read balance for %s in room %s: %v", claims.UserID, claims.RoomID, err)
			}
		}

		c.Send(mustEncode(TypeConnected, ConnectedEvent{
			UserID:      claims.UserID,
			DisplayName: claims.DisplayName,
			RoomID:      claims.RoomID,
			Guest:       claims.Guest,
			Host:        claims.Host,
			Balance:     balance,
		}))

		// Tell the newcomer about every member already in the room —
		// player_joined broadcasts only ever announced future joins, so a
		// client connecting second never otherwise learns who connected
		// first. Reuses the existing message type/schema; sent directly to
		// c, not broadcast, since every other member already knows about
		// themselves.
		for _, m := range room.Members() {
			if m.UserID == claims.UserID {
				continue
			}
			c.Send(mustEncode(TypePlayerJoined, PresenceEvent{
				UserID:      m.UserID,
				DisplayName: m.DisplayName,
				PlayerCount: room.Count(),
			}))
		}

		room.Broadcast(mustEncode(TypePlayerJoined, PresenceEvent{
			UserID:      claims.UserID,
			DisplayName: claims.DisplayName,
			PlayerCount: room.Count(),
		}))

		go c.WritePump()
		go c.ReadPump(onMessage, func() {
			room.Leave(c)
			room.Broadcast(mustEncode(TypePlayerLeft, PresenceEvent{
				UserID:      claims.UserID,
				DisplayName: claims.DisplayName,
				PlayerCount: room.Count(),
			}))
			if sessions != nil {
				sessions.ScheduleEndSession(claims.RoomID, claims.UserID, claims.Guest)
			}
		})
	}
}

// ExtractToken reads a bearer token from the Authorization header
// first, else the token query parameter — browsers cannot set headers
// on a WebSocket handshake, so the query form is required for Phase 6.
// Exported so other packages (a connection-rate-limiting middleware in
// internal/httpapi) can key on the same identity this handler verifies,
// without duplicating the extraction logic.
func ExtractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if rest, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return rest
		}
	}
	return r.URL.Query().Get("token")
}
