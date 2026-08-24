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

func TestGuestSessionBalance(t *testing.T) {
	tests := []struct {
		name  string
		buyIn Tokens
		want  Tokens
	}{
		{name: "a guest joins with exactly the buy-in", buyIn: 500, want: 500},
		{name: "the 3x account multiple never applies to guests", buyIn: MaxBuyIn, want: MaxBuyIn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GuestSessionBalance(tt.buyIn); got != tt.want {
				t.Errorf("GuestSessionBalance(%d) = %d, want %d", tt.buyIn, got, tt.want)
			}
		})
	}
}

func TestAccountSessionBalance(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		roomBuyIn      Tokens
		want           Tokens
	}{
		{
			name:           "a wealthy account is capped at three times the buy-in",
			accountBalance: 10_000,
			roomBuyIn:      500,
			want:           1500,
		},
		{
			name:           "an account below the cap brings its whole balance",
			accountBalance: 800,
			roomBuyIn:      500,
			want:           800,
		},
		{
			name:           "an account short of the buy-in joins partial",
			accountBalance: 200,
			roomBuyIn:      2000,
			want:           200,
		},
		{
			name:           "an account exactly at the cap brings the cap",
			accountBalance: 1500,
			roomBuyIn:      500,
			want:           1500,
		},
		{
			name:           "an empty account brings nothing",
			accountBalance: 0,
			roomBuyIn:      500,
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AccountSessionBalance(tt.accountBalance, tt.roomBuyIn)

			if got != tt.want {
				t.Errorf("AccountSessionBalance(%d, %d) = %d, want %d",
					tt.accountBalance, tt.roomBuyIn, got, tt.want)
			}
		})
	}
}

func TestIsPartialBuyIn(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		roomBuyIn      Tokens
		want           bool
	}{
		{
			name:           "a balance below the buy-in is partial",
			accountBalance: 200,
			roomBuyIn:      2000,
			want:           true,
		},
		{
			name:           "a balance exactly at the buy-in is not partial",
			accountBalance: 2000,
			roomBuyIn:      2000,
			want:           false,
		},
		{
			name:           "a balance above the buy-in is not partial",
			accountBalance: 5000,
			roomBuyIn:      2000,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPartialBuyIn(tt.accountBalance, tt.roomBuyIn)

			if got != tt.want {
				t.Errorf("IsPartialBuyIn(%d, %d) = %v, want %v",
					tt.accountBalance, tt.roomBuyIn, got, tt.want)
			}
		})
	}
}
