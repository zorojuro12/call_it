package redisstore

import (
	"errors"
	"fmt"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// ErrNotFound is returned when a key or hash field this package expects
// is absent. redis.Nil never escapes this package — every read wraps it
// into this sentinel instead, so callers never need to import go-redis
// to handle a not-found case.
var ErrNotFound = errors.New("redisstore: key or field not found")

// ErrAlreadyExists is returned when claim_unique.lua finds its index
// key already claimed — a duplicate email at registration, or a
// colliding room code at creation.
var ErrAlreadyExists = errors.New("redisstore: unique index already claimed")

// ErrPoolLocked is returned when a wager targets a round that is not
// open — either its status has moved on, or the Redis clock has already
// passed its lock instant.
var ErrPoolLocked = errors.New("redisstore: round is locked")

// ErrHostCannotBet is returned when the room's host attempts to place a
// wager in their own room — the conflict of interest the host-cannot-bet
// rule removes (spec §4).
var ErrHostCannotBet = errors.New("redisstore: host cannot wager in their own room")

// ErrNotInRoom is returned when a user with no wallet field in the room
// attempts to place a wager.
var ErrNotInRoom = errors.New("redisstore: user has no wallet in this room")

// ErrRoundTerminal is returned when an operation that requires a
// non-terminal round (e.g. locking) targets one that has already
// resolved or refunded.
var ErrRoundTerminal = errors.New("redisstore: round is already terminal")

// ErrAlreadySettled is returned when SettleRound or RefundRound targets
// a round that has already resolved or refunded — a benign replay must
// credit nothing a second time.
var ErrAlreadySettled = errors.New("redisstore: round is already settled")

// ErrNotLocked is returned when SettleRound or RefundRound targets a
// round that has not been locked — settling an open round would race
// wagers still arriving.
var ErrNotLocked = errors.New("redisstore: round is not locked")

// mapSettleStatus maps settle_round.lua's and refund_round.lua's non-OK
// status codes to Go errors. Later checkpoints add a case per guard as
// each is implemented.
func mapSettleStatus(reply []string) error {
	code := reply[0]
	switch code {
	case "ALREADY_RESOLVED":
		return ErrAlreadySettled
	case "NOT_LOCKED":
		return ErrNotLocked
	default:
		return fmt.Errorf("redisstore: settle round: unrecognized status %q", code)
	}
}

// mapWagerStatus maps place_wager.lua's non-OK status codes to Go
// errors. Task 4 adds a case per guard as each one is implemented; an
// unrecognized code for now returns a wrapped generic error naming it,
// since no guard has been added yet.
func mapWagerStatus(reply []string) error {
	code := reply[0]
	switch code {
	case "POOL_LOCKED":
		return ErrPoolLocked
	case "INVALID_OUTCOME":
		return domain.ErrInvalidOutcome
	case "HOST_CANNOT_BET":
		return ErrHostCannotBet
	case "NOT_IN_ROOM":
		return ErrNotInRoom
	case "INSUFFICIENT_FUNDS":
		return domain.ErrInsufficientFunds
	default:
		return fmt.Errorf("redisstore: place wager: unrecognized status %q", code)
	}
}
