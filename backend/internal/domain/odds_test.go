package domain

import "testing"

func TestMultiplier(t *testing.T) {
	tests := []struct {
		name  string
		total Tokens
		pool  Tokens
		want  float64
	}{
		{name: "a quarter of the total pays four to one", total: 1000, pool: 250, want: 4},
		{name: "an even split pays two to one", total: 1000, pool: 500, want: 2},
		{name: "the only backed outcome pays even money", total: 1000, pool: 1000, want: 1},
		{name: "a tenth of the total pays ten to one", total: 1000, pool: 100, want: 10},
		{name: "an unbacked outcome has no defined multiplier", total: 1000, pool: 0, want: 0},
		{name: "an empty round has no defined multiplier", total: 0, pool: 0, want: 0},
		{name: "a negative pool cannot happen and yields no multiplier", total: 1000, pool: -5, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Multiplier(tt.total, tt.pool); got != tt.want {
				t.Errorf("Multiplier(%d, %d) = %v, want %v", tt.total, tt.pool, got, tt.want)
			}
		})
	}
}

func TestMultiplier_FractionalResult(t *testing.T) {
	// Odds are the one place a float is correct — the payout itself is
	// still floored to whole tokens by Settle.
	got := Multiplier(1000, 300)

	const want = 10.0 / 3.0
	if got != want {
		t.Errorf("Multiplier(1000, 300) = %v, want %v", got, want)
	}
}
