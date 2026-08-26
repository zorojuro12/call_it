package events

import (
	"strings"
	"testing"
)

func TestDecodeWagerPlaced(t *testing.T) {
	fields := map[string]string{
		"type":            "wager_placed",
		"user":            "u1",
		"outcome":         "1",
		"amount":          "100",
		"balance":         "900",
		"idempotency_key": "idem-1",
		"room_id":         "r1",
		"round_id":        "rd1",
	}

	ev, err := Decode(fields)
	if err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}

	wp, ok := ev.(WagerPlaced)
	if !ok {
		t.Fatalf("Decode() = %T, want WagerPlaced", ev)
	}

	want := WagerPlaced{
		RoomID:         "r1",
		RoundID:        "rd1",
		UserID:         "u1",
		IdempotencyKey: "idem-1",
		Outcome:        1,
		Amount:         100,
		Balance:        900,
	}
	if wp != want {
		t.Errorf("Decode() = %+v, want %+v", wp, want)
	}
}

func TestDecodeWagerPlaced_Malformed(t *testing.T) {
	base := map[string]string{
		"type":            "wager_placed",
		"user":            "u1",
		"outcome":         "1",
		"amount":          "100",
		"balance":         "900",
		"idempotency_key": "idem-1",
		"room_id":         "r1",
		"round_id":        "rd1",
	}

	tests := []struct {
		name        string
		mutate      func(map[string]string)
		wantErrName string
	}{
		{
			name:        "amount not an integer",
			mutate:      func(f map[string]string) { f["amount"] = "not-a-number" },
			wantErrName: "amount",
		},
		{
			name:        "outcome not an integer",
			mutate:      func(f map[string]string) { f["outcome"] = "not-a-number" },
			wantErrName: "outcome",
		},
		{
			name:        "room_id absent",
			mutate:      func(f map[string]string) { delete(f, "room_id") },
			wantErrName: "room_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := make(map[string]string, len(base))
			for k, v := range base {
				fields[k] = v
			}
			tt.mutate(fields)

			_, err := Decode(fields)
			if err == nil {
				t.Fatalf("Decode() error = nil, want an error naming %q", tt.wantErrName)
			}
			if !strings.Contains(err.Error(), tt.wantErrName) {
				t.Errorf("Decode() error = %v, want it to mention %q", err, tt.wantErrName)
			}
		})
	}
}
