// Package ws holds CallIt's WebSocket transport and carries no game
// state.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Envelope is the top-level shape of every message sent or received
// over the socket.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

const (
	TypeConnected     = "connected"
	TypePlayerJoined  = "player_joined"
	TypePlayerLeft    = "player_left"
	TypeWagerAccepted = "wager_accepted"
	TypeError         = "error"
)

var ErrMalformed = errors.New("ws: malformed message envelope")
var ErrMissingType = errors.New("ws: message envelope has no type")

// ConnectedEvent is sent once, right after a successful upgrade.
// Balance is the connecting identity's current room wallet, over its
// own connection only — the same narrow disclosure WagerAcceptedEvent
// already makes for its own placer. It is what lets a reload show the
// session's actual current balance instead of the stale value a client
// cached at the original join: a full page reload always re-mounts with
// no memory of any wager placed since, so this is the one place a
// reconnecting client can learn its own live balance without a second
// REST round trip — one a guest identity has no way to repeat anyway
// (a guest has no stable account token to rejoin with; a fresh join
// call would mint a brand-new random identity, not resume this one).
type ConnectedEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	RoomID      string `json:"room_id"`
	Guest       bool   `json:"guest"`
	Host        bool   `json:"host"`
	Balance     int64  `json:"balance"`
}

// PresenceEvent announces a join or leave to the room.
type PresenceEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	PlayerCount int    `json:"player_count"`
}

// WagerAcceptedEvent is a private reply to the placer of a successful
// wager, carrying the placer's own new balance — never another
// player's stake. This discloses only the sender's own stake and
// balance to that sender alone; the room-wide odds_updated broadcast
// stays pool-totals-only.
type WagerAcceptedEvent struct {
	RoundID string `json:"round_id"`
	Outcome int    `json:"outcome"`
	Amount  int64  `json:"amount"`
	Balance int64  `json:"balance"`
}

// ErrorEvent is a private reply to the sender of a bad message.
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Encode marshals data and wraps it in an Envelope of the given type.
func Encode(msgType string, data any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("ws: encode: %w", err)
	}
	return json.Marshal(Envelope{Type: msgType, Data: raw})
}

// Decode unmarshals raw into an Envelope, rejecting malformed JSON and
// a missing or empty type.
func Decode(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("ws: decode: %w", ErrMalformed)
	}
	if env.Type == "" {
		return Envelope{}, ErrMissingType
	}
	return env, nil
}
