package domain

import (
	"errors"
	"testing"
)

func TestRoundStatusTransition_Legal(t *testing.T) {
	tests := []struct {
		name string
		from RoundStatus
		to   RoundStatus
	}{
		{
			name: "open round locks when the countdown expires",
			from: RoundOpen,
			to:   RoundLocked,
		},
		{
			name: "locked round resolves when the host calls the outcome",
			from: RoundLocked,
			to:   RoundResolved,
		},
		{
			name: "locked round refunds when it cannot be resolved",
			from: RoundLocked,
			to:   RoundRefunded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.from.Transition(tt.to)

			if err != nil {
				t.Fatalf("Transition(%s -> %s): unexpected error: %v", tt.from, tt.to, err)
			}
			if got != tt.to {
				t.Errorf("Transition(%s -> %s) = %s, want %s", tt.from, tt.to, got, tt.to)
			}
		})
	}
}

func TestRoundStatusTransition_Illegal(t *testing.T) {
	tests := []struct {
		name string
		from RoundStatus
		to   RoundStatus
	}{
		{
			name: "open cannot skip lockout and resolve directly",
			from: RoundOpen,
			to:   RoundResolved,
		},
		{
			name: "open cannot refund before it locks",
			from: RoundOpen,
			to:   RoundRefunded,
		},
		{
			name: "locked cannot reopen for more wagers",
			from: RoundLocked,
			to:   RoundOpen,
		},
		{
			name: "resolved is terminal and cannot be refunded",
			from: RoundResolved,
			to:   RoundRefunded,
		},
		{
			name: "refunded is terminal and cannot be resolved",
			from: RoundRefunded,
			to:   RoundResolved,
		},
		{
			name: "a status cannot transition to itself",
			from: RoundOpen,
			to:   RoundOpen,
		},
		{
			name: "an unrecognized status has nowhere legal to go",
			from: RoundStatus("garbage"),
			to:   RoundOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.from.Transition(tt.to)

			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Transition(%s -> %s): got error %v, want ErrInvalidTransition", tt.from, tt.to, err)
			}
			if got != tt.from {
				t.Errorf("Transition(%s -> %s) returned status %s, want the unchanged %s", tt.from, tt.to, got, tt.from)
			}
		})
	}
}

func TestRoundStatusIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status RoundStatus
		want   bool
	}{
		{name: "open is not terminal", status: RoundOpen, want: false},
		{name: "locked is not terminal", status: RoundLocked, want: false},
		{name: "resolved is terminal", status: RoundResolved, want: true},
		{name: "refunded is terminal", status: RoundRefunded, want: true},
		{name: "an unrecognized status is terminal", status: RoundStatus("garbage"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("RoundStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestRoundStatusAcceptsWagers(t *testing.T) {
	tests := []struct {
		name   string
		status RoundStatus
		want   bool
	}{
		{name: "open accepts wagers", status: RoundOpen, want: true},
		{name: "locked rejects wagers", status: RoundLocked, want: false},
		{name: "resolved rejects wagers", status: RoundResolved, want: false},
		{name: "refunded rejects wagers", status: RoundRefunded, want: false},
		{name: "an unrecognized status rejects wagers", status: RoundStatus("garbage"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.AcceptsWagers(); got != tt.want {
				t.Errorf("RoundStatus(%q).AcceptsWagers() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestValidateOutcomeCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "two outcomes is the binary minimum", count: 2},
		{name: "three outcomes is allowed", count: 3},
		{name: "four outcomes is the maximum", count: 4},
		{name: "one outcome is not a prediction", count: 1, wantErr: true},
		{name: "zero outcomes is rejected", count: 0, wantErr: true},
		{name: "negative outcomes is rejected", count: -1, wantErr: true},
		{name: "five outcomes exceeds the maximum", count: 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutcomeCount(tt.count)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidOutcomeCount) {
					t.Fatalf("ValidateOutcomeCount(%d) = %v, want ErrInvalidOutcomeCount", tt.count, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateOutcomeCount(%d): unexpected error: %v", tt.count, err)
			}
		})
	}
}

func TestValidateOutcomeIndex(t *testing.T) {
	tests := []struct {
		name    string
		idx     int
		count   int
		wantErr bool
	}{
		{name: "first outcome of two", idx: 0, count: 2},
		{name: "last outcome of four", idx: 3, count: 4},
		{name: "index equal to count is out of range", idx: 2, count: 2, wantErr: true},
		{name: "index beyond count is out of range", idx: 9, count: 4, wantErr: true},
		{name: "negative index is out of range", idx: -1, count: 4, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutcomeIndex(tt.idx, tt.count)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidOutcome) {
					t.Fatalf("ValidateOutcomeIndex(%d, %d) = %v, want ErrInvalidOutcome", tt.idx, tt.count, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateOutcomeIndex(%d, %d): unexpected error: %v", tt.idx, tt.count, err)
			}
		})
	}
}
