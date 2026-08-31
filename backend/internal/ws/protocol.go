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
	TypeConnected    = "connected"
	TypePlayerJoined = "player_joined"
	TypePlayerLeft   = "player_left"
	TypeError        = "error"
)

var ErrMalformed = errors.New("ws: malformed message envelope")
var ErrMissingType = errors.New("ws: message envelope has no type")

// ConnectedEvent is sent once, right after a successful upgrade.
type ConnectedEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	RoomID      string `json:"room_id"`
	Guest       bool   `json:"guest"`
	Host        bool   `json:"host"`
}

// PresenceEvent announces a join or leave to the room.
type PresenceEvent struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	PlayerCount int    `json:"player_count"`
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
