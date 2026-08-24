package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestSettle_SoleWinnerTakesTotal(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0", got.Dust)
	}
	if got.Refunded {
		t.Error("Settle marked the round refunded, want resolved")
	}
}

func TestSettle_ProportionalSplit(t *testing.T) {
	// Winning pool 400 (alice 100, bob 300), losing pool 600.
	// Total 1000, so the multiplier is 2.5x.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 0, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{
		{UserID: "alice", Amount: 250},
		{UserID: "bob", Amount: 750},
	}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0 for an evenly divisible pool", got.Dust)
	}
}

func TestSettle_ThreeOutcomes(t *testing.T) {
	// Winning pool 200 of a 1000 total: a 5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 2, Amount: 200},
		{UserID: "bob", Outcome: 0, Amount: 500},
		{UserID: "carol", Outcome: 1, Amount: 300},
	}

	got, err := Settle(stakes, 2, 3)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
}

func TestSettle_FlooringProducesDust(t *testing.T) {
	// Winning pool 3 (alice 1, bob 2), total 10.
	// alice: floor(1 * 10 / 3) = 3.  bob: floor(2 * 10 / 3) = 6.
	// Paid 9 of 10 — one token of dust.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 1},
		{UserID: "bob", Outcome: 0, Amount: 2},
		{UserID: "carol", Outcome: 1, Amount: 7},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{
		{UserID: "alice", Amount: 3},
		{UserID: "bob", Amount: 6},
	}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 1 {
		t.Errorf("Settle dust = %d, want 1", got.Dust)
	}
}

func TestSettle_DustNeverExceedsWinnerCount(t *testing.T) {
	// Flooring can lose at most one token per winning stake, so dust is
	// strictly bounded by the number of winners. A larger remainder
	// means tokens are being lost somewhere other than rounding.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 7},
		{UserID: "bob", Outcome: 0, Amount: 11},
		{UserID: "carol", Outcome: 0, Amount: 13},
		{UserID: "dave", Outcome: 1, Amount: 101},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if got.Dust >= 3 {
		t.Errorf("Settle dust = %d, want less than the winner count 3", got.Dust)
	}

	var paid Tokens
	for _, p := range got.Payouts {
		paid += p.Amount
	}
	if paid+got.Dust != 132 {
		t.Errorf("payouts %d + dust %d = %d, want the 132-token total", paid, got.Dust, paid+got.Dust)
	}
}

func TestSettle_LosersReceiveNothing(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 500},
		{UserID: "bob", Outcome: 1, Amount: 200},
		{UserID: "carol", Outcome: 2, Amount: 300},
		{UserID: "dave", Outcome: 3, Amount: 400},
	}

	got, err := Settle(stakes, 0, 4)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if len(got.Payouts) != 1 {
		t.Fatalf("Settle produced %d payouts, want 1 — losers must produce no credit", len(got.Payouts))
	}
	if got.Payouts[0].UserID != "alice" {
		t.Errorf("Settle credited %q, want alice", got.Payouts[0].UserID)
	}
}

func TestSettle_PlayerBackingBothSidesIsPaidOnlyOnTheWinner(t *testing.T) {
	// A player may hold stakes on several outcomes; only the winning one
	// pays. Total 1000, winning pool 400, so a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 400},
		{UserID: "alice", Outcome: 1, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 500},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
}

func TestSettle_PlayerResults(t *testing.T) {
	// Total 1000, winning pool 400, so a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 0, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []PlayerResult{
		{UserID: "alice", Staked: 100, Returned: 250, Net: 150},
		{UserID: "bob", Staked: 300, Returned: 750, Net: 450},
		{UserID: "carol", Staked: 600, Returned: 0, Net: -600},
	}
	if !reflect.DeepEqual(got.Results, want) {
		t.Errorf("Settle results = %+v, want %+v", got.Results, want)
	}
}

func TestSettle_PlayerResultsAggregateAcrossOutcomes(t *testing.T) {
	// alice hedges across both outcomes and must appear once, with her
	// stakes summed. Total 1000, winning pool 400: a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 400},
		{UserID: "alice", Outcome: 1, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 500},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []PlayerResult{
		{UserID: "alice", Staked: 500, Returned: 1000, Net: 500},
		{UserID: "bob", Staked: 500, Returned: 0, Net: -500},
	}
	if !reflect.DeepEqual(got.Results, want) {
		t.Errorf("Settle results = %+v, want %+v", got.Results, want)
	}
}

func TestSettle_PlayerResultsNetSumsToNegativeDust(t *testing.T) {
	// Every token a winner gains is a token a loser lost, except what
	// flooring strands as dust. So the players' nets must sum to exactly
	// minus the dust.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 1},
		{UserID: "bob", Outcome: 0, Amount: 2},
		{UserID: "carol", Outcome: 1, Amount: 7},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	var netSum Tokens
	for _, r := range got.Results {
		netSum += r.Net
	}
	if netSum != -got.Dust {
		t.Errorf("player nets sum to %d, want %d (minus the dust)", netSum, -got.Dust)
	}
}

func TestSettle_RejectsInvalidWinningOutcome(t *testing.T) {
	tests := []struct {
		name           string
		winningOutcome int
		outcomeCount   int
	}{
		{name: "winning index equal to the outcome count", winningOutcome: 2, outcomeCount: 2},
		{name: "winning index beyond the outcome count", winningOutcome: 7, outcomeCount: 3},
		{name: "negative winning index", winningOutcome: -1, outcomeCount: 2},
	}

	stakes := []Stake{{UserID: "alice", Outcome: 0, Amount: 100}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Settle(stakes, tt.winningOutcome, tt.outcomeCount)

			if !errors.Is(err, ErrInvalidOutcome) {
				t.Fatalf("Settle(_, %d, %d) = %v, want ErrInvalidOutcome",
					tt.winningOutcome, tt.outcomeCount, err)
			}
		})
	}
}

func TestSettle_RejectsStakeOnOutcomeTheRoundLacks(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 5, Amount: 100},
	}

	_, err := Settle(stakes, 0, 2)

	if !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("Settle = %v, want ErrInvalidOutcome", err)
	}
}

func TestSettle_RejectsNonPositiveStakes(t *testing.T) {
	tests := []struct {
		name   string
		amount Tokens
	}{
		{name: "a zero stake", amount: 0},
		{name: "a negative stake would mint tokens", amount: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stakes := []Stake{
				{UserID: "alice", Outcome: 0, Amount: 100},
				{UserID: "bob", Outcome: 1, Amount: tt.amount},
			}

			_, err := Settle(stakes, 0, 2)

			if !errors.Is(err, ErrInvalidStake) {
				t.Fatalf("Settle with a %d stake = %v, want ErrInvalidStake", tt.amount, err)
			}
		})
	}
}

func TestSettle_EmptyRound(t *testing.T) {
	// A round nobody wagered in has an empty winning pool, so it takes
	// the refund path — with nothing to refund.
	got, err := Settle(nil, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if len(got.Payouts) != 0 {
		t.Errorf("Settle produced %d payouts for an empty round, want 0", len(got.Payouts))
	}
	if len(got.Results) != 0 {
		t.Errorf("Settle produced %d results for an empty round, want 0", len(got.Results))
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0", got.Dust)
	}
}
