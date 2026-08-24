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

func TestCanRefill_Quota(t *testing.T) {
	tests := []struct {
		name           string
		claimsInWindow int
		wantErr        bool
	}{
		{name: "no claims used yet", claimsInWindow: 0},
		{name: "one claim used", claimsInWindow: 1},
		{name: "the last available claim", claimsInWindow: RefillQuota - 1},
		{name: "the quota is exhausted", claimsInWindow: RefillQuota, wantErr: true},
		{name: "somehow over quota is still exhausted", claimsInWindow: RefillQuota + 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanRefill(0, tt.claimsInWindow)

			if tt.wantErr {
				if !errors.Is(err, ErrRefillQuotaExhausted) {
					t.Fatalf("CanRefill(0, %d) = %v, want ErrRefillQuotaExhausted", tt.claimsInWindow, err)
				}
				return
			}
			if err != nil {
				t.Errorf("CanRefill(0, %d): unexpected error: %v", tt.claimsInWindow, err)
			}
		})
	}
}

func TestCanRefill_BalanceCheckedBeforeQuota(t *testing.T) {
	// An ineligible balance should report why it is ineligible rather
	// than blaming the quota, so the UI can say the useful thing.
	err := CanRefill(RefillTarget, RefillQuota)

	if !errors.Is(err, ErrRefillNotEligible) {
		t.Fatalf("CanRefill(%d, %d) = %v, want ErrRefillNotEligible", RefillTarget, RefillQuota, err)
	}
}
