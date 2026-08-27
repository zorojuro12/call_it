package events

import (
	"encoding/json"
	"testing"
)

func TestEventJSONWireFormat(t *testing.T) {
	// Checkpoint 1: both event types marshal to explicit snake_case JSON
	tests := []struct {
		name     string
		event    Event
		expected string
	}{
		{
			name: "WagerPlaced marshals to snake_case JSON",
			event: WagerPlaced{
				RoomID:         "r1",
				RoundID:        "rd1",
				UserID:         "u1",
				IdempotencyKey: "k1",
				Outcome:        1,
				Amount:         50,
				Balance:        950,
			},
			expected: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}`,
		},
		{
			name: "RoundSettled marshals to snake_case JSON",
			event: RoundSettled{
				RoomID:         "r1",
				RoundID:        "rd1",
				IdempotencyKey: "k2",
				WinningOutcome: 1,
				Total:          100,
				Dust:           2,
				Payouts:        []Payout{{UserID: "u1", Amount: 98}},
				Refunded:       false,
			},
			expected: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			got := string(b)
			if got != tt.expected {
				t.Errorf("JSON mismatch\ngot:      %s\nexpected: %s", got, tt.expected)
			}
		})
	}
}
