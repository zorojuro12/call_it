package domain

// Stake is one participant's committed tokens on one outcome of a round.
// It mirrors a single field of the round:{roundID}:wagers hash, whose key
// is "{userID}:{outcomeIdx}" (plan §4) — so (UserID, Outcome) is unique
// within a round and a player's repeat wagers on one outcome arrive
// already summed. A player may hold stakes on several different outcomes.
type Stake struct {
	UserID  string
	Outcome int
	Amount  Tokens
}

// Payout is one credit produced by settling a round: exactly one balance
// movement to apply.
type Payout struct {
	UserID string
	Amount Tokens
}

// Settlement is the complete result of settling a round.
type Settlement struct {
	// Payouts is one credit per settled stake, in the order the stakes
	// were supplied. Settling the same round twice yields identical
	// output.
	Payouts []Payout

	// Dust is the remainder that flooring each payout leaves behind. It
	// is credited to the system_dust ledger account so debits and
	// credits still balance exactly (plan §5) — it must never be
	// silently dropped.
	Dust Tokens

	// Refunded is true when nobody backed the winning outcome. Every
	// stake goes back in full and the round ends RoundRefunded rather
	// than RoundResolved.
	Refunded bool
}

// Settle computes the pari-mutuel result of a round. Each backer of the
// winning outcome receives floor(stake * total / winningPool); whatever
// flooring leaves over becomes Dust.
//
// outcomeCount is the round's declared number of outcomes, used to
// reject a winning index the round never had.
func Settle(stakes []Stake, winningOutcome, outcomeCount int) (Settlement, error) {
	var total, winningPool Tokens
	for _, s := range stakes {
		total += s.Amount
		if s.Outcome == winningOutcome {
			winningPool += s.Amount
		}
	}

	payouts := make([]Payout, 0, len(stakes))
	var paid Tokens
	for _, s := range stakes {
		if s.Outcome != winningOutcome {
			continue
		}
		// Safe in int64 by a wide margin: the largest single stake is
		// StakeCapMultiple * MaxBuyIn = 30,000, so stake * total stays
		// many orders of magnitude below overflow.
		amount := s.Amount * total / winningPool
		payouts = append(payouts, Payout{UserID: s.UserID, Amount: amount})
		paid += amount
	}

	return Settlement{Payouts: payouts, Dust: total - paid}, nil
}
