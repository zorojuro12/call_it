package ws

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
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

// SessionEnder folds a departing account holder's net session result
// into their persistent balance — *round.Service satisfies this.
// Declared here (not imported as a concrete type) so Handler does not
// need a second entry point for tests that don't care about it.
type SessionEnder interface {
	EndSession(ctx context.Context, roomID, userID string, guest bool) (domain.Tokens, error)
}

// Handler builds an http.HandlerFunc that authenticates a room-scoped
// JWT, upgrades the connection, and wires the resulting client into
// hub. onMessage is the seam Phase 4b fills for gameplay; a nil value
// makes every inbound message an unknown-type error reply. sessions is
// called on disconnect to end the departing client's session; a nil
// value skips that step (every existing 4a test that doesn't care
// about it passes nil).
func Handler(hub *Hub, issuer *auth.Issuer, cfg ClientConfig, onMessage MessageHandler, sessions SessionEnder, opts ...HandlerOption) http.HandlerFunc {
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

		ident := Identity{UserID: claims.UserID, DisplayName: claims.DisplayName, Guest: claims.Guest}
		c := NewClient(conn, ident, cfg)
		c.RoomID = claims.RoomID
		room := hub.Join(claims.RoomID, c)

		c.Send(mustEncode(TypeConnected, ConnectedEvent{
			UserID:      claims.UserID,
			DisplayName: claims.DisplayName,
			RoomID:      claims.RoomID,
			Guest:       claims.Guest,
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
				if _, err := sessions.EndSession(context.Background(), claims.RoomID, claims.UserID, claims.Guest); err != nil {
					log.Printf("ws: end session for %s in room %s: %v", claims.UserID, claims.RoomID, err)
				}
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
