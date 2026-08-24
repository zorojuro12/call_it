package domain

// Multiplier is the pari-mutuel payout multiplier for one outcome: the
// total pool divided by that outcome's pool (spec §4). This is the one
// place in the domain where a float is correct — everything stored stays
// a whole token count, and Settle floors the actual payout.
//
// An outcome nobody has backed has no defined multiplier and returns 0.
// The sentinel is unambiguous because a real multiplier is never below
// 1: an outcome's pool is always part of the total.
func Multiplier(total, pool Tokens) float64 {
	if pool <= 0 {
		return 0
	}
	return float64(total) / float64(pool)
}
