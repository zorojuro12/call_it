// Package room orchestrates room lifecycle — creation with a generated
// short code, and joining by code as a guest or an account holder —
// against internal/redisstore's writers.
package room

import "errors"

// ErrNotJoinable is returned when a room exists but its status is not
// "open" — e.g. its round has already locked or the room has closed.
var ErrNotJoinable = errors.New("room: room is not open for joining")

// ErrCodeExhausted is returned when room creation could not find an
// unused code within its retry bound.
var ErrCodeExhausted = errors.New("room: could not generate an unused room code")
