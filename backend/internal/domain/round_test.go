package domain

import "testing"

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
