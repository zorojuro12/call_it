package domain

import "fmt"

// CanRefill reports whether an account may claim a manual refill right
// now. Eligibility is simply "below the target" — there is no separate
// threshold constant, because two names for one number invite a future
// cleanup that makes them diverge (see this phase's plan, §A2).
//
// claimsInWindow is supplied by the caller, counted by the Redis
// sliding-window limiter in Phase 2. This function owns the policy, not
// the counting, which is what keeps it testable with nothing running.
func CanRefill(balance Tokens, claimsInWindow int) error {
	if balance >= RefillTarget {
		return fmt.Errorf("%w: balance %d, target %d", ErrRefillNotEligible, balance, RefillTarget)
	}
	return nil
}
