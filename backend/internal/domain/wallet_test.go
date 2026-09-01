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

func TestValidateStake_NonPositive(t *testing.T) {
	tests := []struct {
		name   string
		amount Tokens
	}{
		{name: "a zero stake is not a wager", amount: 0},
		{name: "a negative stake would mint tokens", amount: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStake(tt.amount, 1000)

			if !errors.Is(err, ErrInvalidStake) {
				t.Fatalf("ValidateStake(%d, 1000) = %v, want ErrInvalidStake", tt.amount, err)
			}
		})
	}
}

func TestValidateStake_Valid(t *testing.T) {
	tests := []struct {
		name           string
		amount         Tokens
		sessionBalance Tokens
	}{
		{name: "a stake below the balance is accepted", amount: 100, sessionBalance: 1000},
		{name: "a stake equal to the balance is accepted", amount: 1000, sessionBalance: 1000},
		{name: "the smallest possible stake is accepted", amount: 1, sessionBalance: 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateStake(tt.amount, tt.sessionBalance); err != nil {
				t.Errorf("ValidateStake(%d, %d): unexpected error: %v", tt.amount, tt.sessionBalance, err)
			}
		})
	}
}

func TestValidateStake_InsufficientFunds(t *testing.T) {
	tests := []struct {
		name           string
		amount         Tokens
		sessionBalance Tokens
	}{
		{name: "one token more than the balance", amount: 1001, sessionBalance: 1000},
		{name: "far more than the balance", amount: 50_000, sessionBalance: 1000},
		{name: "any stake against an empty wallet", amount: 1, sessionBalance: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStake(tt.amount, tt.sessionBalance)

			if !errors.Is(err, ErrInsufficientFunds) {
				t.Fatalf("ValidateStake(%d, %d) = %v, want ErrInsufficientFunds",
					tt.amount, tt.sessionBalance, err)
			}
		})
	}
}

func TestValidateStakeAmount(t *testing.T) {
	tests := []struct {
		name    string
		amount  Tokens
		wantErr bool
	}{
		{name: "the smallest positive stake is accepted", amount: 1},
		{name: "a large stake is accepted", amount: 1_000_000},
		{name: "a zero stake is not a wager", amount: 0, wantErr: true},
		{name: "a negative stake would mint tokens", amount: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStakeAmount(tt.amount)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidStake) {
					t.Fatalf("ValidateStakeAmount(%d) = %v, want ErrInvalidStake", tt.amount, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateStakeAmount(%d): unexpected error: %v", tt.amount, err)
			}
		})
	}
}

func TestValidateStake_DelegatesSignRule(t *testing.T) {
	if err := ValidateStake(-5, 1000); !errors.Is(err, ErrInvalidStake) {
		t.Fatalf("ValidateStake(-5, 1000) = %v, want ErrInvalidStake", err)
	}
}

func TestApplySessionResult(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		sessionStart   Tokens
		sessionEnd     Tokens
		want           Tokens
	}{
		{
			name:           "a winning session adds only the net gain",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     1600,
			want:           1600,
		},
		{
			name:           "a partial buy-in win adds the gain, not the session total",
			accountBalance: 300,
			sessionStart:   300,
			sessionEnd:     900,
			want:           900,
		},
		{
			name:           "a capped session adds the gain on top of the untouched balance",
			accountBalance: 10_000,
			sessionStart:   1500,
			sessionEnd:     2400,
			want:           10_900,
		},
		{
			name:           "a losing session subtracts only the net loss",
			accountBalance: 10_000,
			sessionStart:   1500,
			sessionEnd:     400,
			want:           8900,
		},
		{
			name:           "a total wipeout of a full-balance session floors at zero",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     0,
			want:           0,
		},
		{
			name:           "a break-even session changes nothing",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     1000,
			want:           1000,
		},
		{
			name:           "an inconsistent caller cannot drive the balance negative",
			accountBalance: 100,
			sessionStart:   5000,
			sessionEnd:     0,
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplySessionResult(tt.accountBalance, tt.sessionStart, tt.sessionEnd)

			if got != tt.want {
				t.Errorf("ApplySessionResult(%d, %d, %d) = %d, want %d",
					tt.accountBalance, tt.sessionStart, tt.sessionEnd, got, tt.want)
			}
		})
	}
}
