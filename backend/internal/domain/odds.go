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

// Multipliers computes the multiplier for every outcome in index order,
// which is the shape broadcast to every client in the room whenever the
// odds move (spec §5 step 3). It reads pool totals only and never sees a
// per-user position, which is what keeps the live-odds broadcast
// compatible with wager anonymity.
func Multipliers(total Tokens, pools []Tokens) []float64 {
	board := make([]float64, len(pools))
	for i, pool := range pools {
		board[i] = Multiplier(total, pool)
	}
	return board
}
