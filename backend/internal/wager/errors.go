package wager

import (
	"errors"
	"time"
)

// ErrNoActiveRound is returned when a wager targets a room with no open
// round.
var ErrNoActiveRound = errors.New("wager: the room has no open round")

// ErrBadIdempotency is returned when a wager's idempotency key is not a
// UUIDv4.
var ErrBadIdempotency = errors.New("wager: idempotency key must be a UUIDv4")

// ErrRateLimited is the sentinel a throttled wager wraps.
var ErrRateLimited = errors.New("wager: too many wagers, slow down")

// RateLimitError wraps ErrRateLimited with the retry hint a caller
// needs — the same pattern internal/account's QuotaError uses.
// errors.Is still matches the wrapped sentinel; errors.As retrieves
// this type for the hint.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
