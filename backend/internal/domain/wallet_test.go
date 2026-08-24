package domain

import (
	"errors"
	"testing"
)

func TestValidateBuyIn(t *testing.T) {
	tests := []struct {
		name    string
		buyIn   Tokens
		wantErr bool
	}{
		{name: "the minimum buy-in is allowed", buyIn: MinBuyIn},
		{name: "the maximum buy-in is allowed", buyIn: MaxBuyIn},
		{name: "a mid-range buy-in is allowed", buyIn: 2500},
		{name: "one token below the minimum is rejected", buyIn: MinBuyIn - 1, wantErr: true},
		{name: "one token above the maximum is rejected", buyIn: MaxBuyIn + 1, wantErr: true},
		{name: "a zero buy-in puts nothing at stake", buyIn: 0, wantErr: true},
		{name: "a negative buy-in is rejected", buyIn: -500, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBuyIn(tt.buyIn)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBuyIn) {
					t.Fatalf("ValidateBuyIn(%d) = %v, want ErrInvalidBuyIn", tt.buyIn, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateBuyIn(%d): unexpected error: %v", tt.buyIn, err)
			}
		})
	}
}
