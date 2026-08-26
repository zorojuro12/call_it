package ws

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/auth"
)

var upgrader = websocket.Upgrader{}

// Handler builds an http.HandlerFunc that authenticates a room-scoped
// JWT, upgrades the connection, and wires the resulting client into
// hub. onMessage is the seam Phase 4b fills for gameplay; a nil value
// makes every inbound message an unknown-type error reply.
func Handler(hub *Hub, issuer *auth.Issuer, cfg ClientConfig, onMessage MessageHandler) http.HandlerFunc {
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
