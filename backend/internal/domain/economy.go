package domain

// Economy constants. These are platform invariants rather than
// deployment configuration — none of them is tunable per environment —
// so they live here rather than in internal/config, which loads and
// validates environment variables. Plan §8 originally placed them in
// config; see docs/plans/2026-08-23-phase-1-domain-core.md §A1.
const (
	// StartingBalance is credited once, when an account registers. Its
	// consumer arrives in Phase 3; it is defined here so the whole
	// economy reads in one place.
	StartingBalance Tokens = 1000

	// RefillTarget is the balance a manual refill tops an account up to,
	// and equally the ceiling below which a refill may be claimed at
	// all. One number in two roles: an account under the target may
	// claim, and claiming brings it exactly to the target.
	RefillTarget Tokens = 1000

	// RefillQuota is how many refills an account may claim per rolling
	// seven-day window. The window is counted by the Redis
	// sliding-window limiter in Phase 2; this package owns the policy
	// only, not the counting.
	RefillQuota int = 3

	// MinBuyIn and MaxBuyIn bound the buy-in a host may set at room
	// creation. The ceiling is deliberately on the same order as
	// RefillTarget: far above it, top-stakes rooms become unreachable
	// for anyone not already wealthy.
	MinBuyIn Tokens = 100
	MaxBuyIn Tokens = 10_000

	// StakeCapMultiple is how many times the room's buy-in an account
	// holder may bring into a room, bounded by what they actually hold.
	StakeCapMultiple Tokens = 3
)
