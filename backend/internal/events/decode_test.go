package events

import (
	"reflect"
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

func TestDecodeRoundSettled(t *testing.T) {
	fields := map[string]string{
		"type":            "round_settled",
		"round_id":        "rd1",
		"room_id":         "r1",
		"dust":            "2",
		"total":           "400",
		"winning_outcome": "1",
		"payouts":         `[{"user_id":"u2","amount":398}]`,
		"idempotency_key": "idem-2",
	}

	ev, err := Decode(fields)
	if err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}

	rs, ok := ev.(RoundSettled)
	if !ok {
		t.Fatalf("Decode() = %T, want RoundSettled", ev)
	}

	want := RoundSettled{
		RoomID:         "r1",
		RoundID:        "rd1",
		IdempotencyKey: "idem-2",
		WinningOutcome: 1,
		Total:          400,
		Dust:           2,
		Payouts:        []Payout{{UserID: "u2", Amount: 398}},
		Refunded:       false,
	}
	if !reflect.DeepEqual(rs, want) {
		t.Errorf("Decode() = %+v, want %+v", rs, want)
	}
}

func TestDecodeRoundSettled_PayoutPrecision(t *testing.T) {
	// A payout amount of 2^53 + 1 must round-trip exactly. Decoding
	// through interface{}/float64 (e.g. encoding/json's default numeric
	// type) would silently lose precision above 2^53 — this pins the
	// Global Constraint against ever doing that.
	fields := map[string]string{
		"type":            "round_settled",
		"round_id":        "rd1",
		"room_id":         "r1",
		"dust":            "0",
		"total":           "9007199254740993",
		"winning_outcome": "0",
		"payouts":         `[{"user_id":"u1","amount":9007199254740993}]`,
		"idempotency_key": "idem-3",
	}

	ev, err := Decode(fields)
	if err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	rs := ev.(RoundSettled)

	const want int64 = 9007199254740993
	if rs.Total != want {
		t.Errorf("Total = %d, want %d", rs.Total, want)
	}
	if len(rs.Payouts) != 1 || rs.Payouts[0].Amount != want {
		t.Errorf("Payouts = %+v, want one payout with amount %d", rs.Payouts, want)
	}
}

func TestDecodeRoundSettled_Malformed(t *testing.T) {
	base := map[string]string{
		"type":            "round_settled",
		"round_id":        "rd1",
		"room_id":         "r1",
		"dust":            "2",
		"total":           "400",
		"winning_outcome": "1",
		"payouts":         `[{"user_id":"u2","amount":398}]`,
		"idempotency_key": "idem-2",
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"payouts not valid JSON", func(f map[string]string) { f["payouts"] = "not-json" }},
		{"payouts absent", func(f map[string]string) { delete(f, "payouts") }},
		{"dust not an integer", func(f map[string]string) { f["dust"] = "not-a-number" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := make(map[string]string, len(base))
			for k, v := range base {
				fields[k] = v
			}
			tt.mutate(fields)

			if _, err := Decode(fields); err == nil {
				t.Errorf("Decode() error = nil, want an error")
			}
		})
	}
}
