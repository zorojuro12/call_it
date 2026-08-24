package domain

import "errors"

// The domain's failure vocabulary. Callers match with errors.Is and map
// to wire codes at the boundary. This list is deliberately limited to
// failures this package can actually produce — the remaining Lua return
// codes (POOL_LOCKED, HOST_CANNOT_BET, NOT_IN_ROOM) gain Go counterparts
// in Phase 2, when something here returns them.
var (
	ErrInvalidTransition    = errors.New("domain: invalid round status transition")
	ErrInvalidOutcomeCount  = errors.New("domain: round outcome count out of range")
	ErrInvalidOutcome       = errors.New("domain: outcome index out of range")
	ErrInvalidBuyIn         = errors.New("domain: room buy-in out of range")
	ErrInvalidStake         = errors.New("domain: stake must be positive")
	ErrInsufficientFunds    = errors.New("domain: stake exceeds available balance")
	ErrRefillNotEligible    = errors.New("domain: balance is not below the refill target")
	ErrRefillQuotaExhausted = errors.New("domain: refill quota exhausted for the current window")
)
