package domain

import (
	"fmt"
	"testing"
)

// FuzzSettleConservesTokens asserts the invariant the whole ledger rests
// on: settling a round neither creates nor destroys tokens. Whatever is
// not paid out must be accounted for as dust, and every player's net
// must add up to exactly minus that dust.
//
// The corpus byte string is decoded as: one byte selecting the winning
// outcome, then pairs of bytes, each a (outcome, amount) stake.
func FuzzSettleConservesTokens(f *testing.F) {
	f.Add([]byte{0})                   // a round with no wagers
	f.Add([]byte{0, 0, 100, 1, 200})   // one winner, one loser
	f.Add([]byte{2, 0, 100, 1, 200})   // nobody backs the winner
	f.Add([]byte{0, 0, 1, 0, 2, 1, 7}) // flooring strands dust
	f.Add([]byte{1, 0, 255, 1, 255, 2, 255, 3, 255})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}
		const outcomeCount = 4
		winningOutcome := int(data[0]) % outcomeCount

		var stakes []Stake
		var total Tokens
		for i := 1; i+1 < len(data); i += 2 {
			amount := Tokens(data[i+1])
			if amount == 0 {
				// Settle rejects non-positive stakes by contract, which
				// Task 5 covers directly. Skip them so this test stays
				// focused on conservation.
				continue
			}
			stakes = append(stakes, Stake{
				UserID:  fmt.Sprintf("u%d", i),
				Outcome: int(data[i]) % outcomeCount,
				Amount:  amount,
			})
			total += amount
		}

		got, err := Settle(stakes, winningOutcome, outcomeCount)
		if err != nil {
			t.Fatalf("Settle returned an error for structurally valid input: %v", err)
		}

		var distributed Tokens
		for _, p := range got.Payouts {
			if p.Amount < 0 {
				t.Fatalf("Settle produced a negative payout %d for %s", p.Amount, p.UserID)
			}
			distributed += p.Amount
		}

		if distributed+got.Dust != total {
			t.Fatalf("tokens not conserved: payouts %d + dust %d = %d, want the %d staked",
				distributed, got.Dust, distributed+got.Dust, total)
		}
		if got.Dust < 0 {
			t.Fatalf("Settle produced negative dust %d, which would mint tokens", got.Dust)
		}
		if got.Refunded && got.Dust != 0 {
			t.Fatalf("a refunded round stranded %d dust, want 0 — refunds are exact", got.Dust)
		}

		var netSum, stakedSum Tokens
		for _, r := range got.Results {
			netSum += r.Net
			stakedSum += r.Staked
		}
		if stakedSum != total {
			t.Fatalf("player results account for %d staked, want %d", stakedSum, total)
		}
		if netSum != -got.Dust {
			t.Fatalf("player nets sum to %d, want %d (minus the dust)", netSum, -got.Dust)
		}
	})
}
