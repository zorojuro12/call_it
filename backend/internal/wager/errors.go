package wager

import "errors"

// ErrNoActiveRound is returned when a wager targets a room with no open
// round.
var ErrNoActiveRound = errors.New("wager: the room has no open round")

// ErrBadIdempotency is returned when a wager's idempotency key is not a
// UUIDv4.
var ErrBadIdempotency = errors.New("wager: idempotency key must be a UUIDv4")
