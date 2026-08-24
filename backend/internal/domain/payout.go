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

// PlayerResult is one row of the post-round reveal: what a player
// committed, what came back, and what they actually earned. Net is the
// number a player cares about — stake 100 on the winner at 2.5x and you
// earned 150, not the 250 the ledger moves. Losers appear here with a
// negative Net even though they produce no Payout at all.
//
// Wagers are private until the round reaches a terminal state (see this
// phase's plan), so this slice is the first moment anyone learns who
// backed what.
type PlayerResult struct {
	UserID   string
	Staked   Tokens
	Returned Tokens
	Net      Tokens
}

// Settlement is the complete result of settling a round.
type Settlement struct {
	// Payouts is one credit per settled stake, in the order the stakes
	// were supplied. Settling the same round twice yields identical
	// output.
	Payouts []Payout

	// Results is the one-row-per-player summary revealed once the round
	// closes. Players appear in the order they first staked.
	Results []PlayerResult

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

	return Settlement{
		Payouts: payouts,
		Results: playerResults(stakes, payouts),
		Dust:    total - paid,
	}, nil
}

// playerResults folds per-stake credits into the one-row-per-player
// summary the reveal shows. Players appear in the order they first
// staked rather than in map order, so the same round always produces the
// same slice.
func playerResults(stakes []Stake, payouts []Payout) []PlayerResult {
	index := make(map[string]int, len(stakes))
	results := make([]PlayerResult, 0, len(stakes))

	for _, s := range stakes {
		i, seen := index[s.UserID]
		if !seen {
			i = len(results)
			index[s.UserID] = i
			results = append(results, PlayerResult{UserID: s.UserID})
		}
		results[i].Staked += s.Amount
	}

	// Every payout derives from a stake, so its user is already indexed.
	for _, p := range payouts {
		results[index[p.UserID]].Returned += p.Amount
	}

	for i := range results {
		results[i].Net = results[i].Returned - results[i].Staked
	}

	return results
}
