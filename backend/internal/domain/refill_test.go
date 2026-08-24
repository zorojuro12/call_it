package domain

import (
	"errors"
	"testing"
)

func TestCanRefill_Balance(t *testing.T) {
	tests := []struct {
		name    string
		balance Tokens
		wantErr bool
	}{
		{name: "an empty account may refill", balance: 0},
		{name: "an account well below target may refill", balance: 150},
		{name: "one token below target may refill", balance: RefillTarget - 1},
		{name: "an account exactly at target has nothing to claim", balance: RefillTarget, wantErr: true},
		{name: "an account above target has nothing to claim", balance: 5000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanRefill(tt.balance, 0)

			if tt.wantErr {
				if !errors.Is(err, ErrRefillNotEligible) {
					t.Fatalf("CanRefill(%d, 0) = %v, want ErrRefillNotEligible", tt.balance, err)
				}
				return
			}
			if err != nil {
				t.Errorf("CanRefill(%d, 0): unexpected error: %v", tt.balance, err)
			}
		})
	}
}
