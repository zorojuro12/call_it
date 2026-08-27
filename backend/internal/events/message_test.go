package events

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

func TestDecodeMessage(t *testing.T) {
	// Checkpoint 2: DecodeMessage round-trips both types, routing by topic
	wager := WagerPlaced{
		RoomID:         "r1",
		RoundID:        "rd1",
		UserID:         "u1",
		IdempotencyKey: "k1",
		Outcome:        1,
		Amount:         50,
		Balance:        950,
	}
	wagerJSON, _ := json.Marshal(wager)

	settled := RoundSettled{
		RoomID:         "r1",
		RoundID:        "rd1",
		IdempotencyKey: "k2",
		WinningOutcome: 1,
		Total:          100,
		Dust:           2,
		Payouts:        []Payout{{UserID: "u1", Amount: 98}},
		Refunded:       false,
	}
	settledJSON, _ := json.Marshal(settled)

	refund := RoundSettled{
		RoomID:         "r1",
		RoundID:        "rd1",
		IdempotencyKey: "k3",
		WinningOutcome: -1,
		Total:          100,
		Dust:           0,
		Payouts:        []Payout{{UserID: "u1", Amount: 100}},
		Refunded:       true,
	}
	refundJSON, _ := json.Marshal(refund)

	tests := []struct {
		name    string
		topic   string
		payload []byte
		want    Event
		wantErr bool
	}{
		{
			name:    "DecodeMessage routes WagerPlaced to wagers-placed",
			topic:   TopicWagersPlaced,
			payload: wagerJSON,
			want:    wager,
			wantErr: false,
		},
		{
			name:    "DecodeMessage routes RoundSettled to rounds-settled",
			topic:   TopicRoundsSettled,
			payload: settledJSON,
			want:    settled,
			wantErr: false,
		},
		{
			name:    "DecodeMessage routes refund to rounds-settled",
			topic:   TopicRoundsSettled,
			payload: refundJSON,
			want:    refund,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeMessage(tt.topic, tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeMessage error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeMessage got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeMessageUnknownTopic(t *testing.T) {
	// Checkpoint 3: an unrecognised topic is an error, never a skip
	topic := "some-other-topic"
	ev, err := DecodeMessage(topic, []byte("{}"))
	if ev != nil {
		t.Errorf("DecodeMessage returned non-nil event for unknown topic: %#v", ev)
	}
	if err == nil {
		t.Errorf("DecodeMessage returned nil error for unknown topic")
		return
	}
	if !errors.Is(err, ErrUnknownEventType) {
		t.Errorf("DecodeMessage error = %v, want errors.Is(err, ErrUnknownEventType)", err)
	}
	if !strings.Contains(err.Error(), topic) {
		t.Errorf("DecodeMessage error message %q does not contain topic %q", err.Error(), topic)
	}
}

func TestDecodeMessageRejectsUnknownField(t *testing.T) {
	// Checkpoint 4: unknown JSON fields are rejected, not ignored
	// This is the drift guard for a money wire format. A producer that renamed
	// `amount` to `stake` would otherwise decode to `Amount: 0` and write a
	// zero-token ledger entry rather than failing loudly.
	payload := []byte(`{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950,"surprise":7}`)
	ev, err := DecodeMessage(TopicWagersPlaced, payload)
	if ev != nil {
		t.Errorf("DecodeMessage returned non-nil event for unknown field: %#v", ev)
	}
	if err == nil {
		t.Errorf("DecodeMessage returned nil error for unknown field")
		return
	}
	if !strings.Contains(err.Error(), "surprise") {
		t.Errorf("DecodeMessage error message %q does not contain unknown field name %q", err.Error(), "surprise")
	}
}

func TestDecodeMessageValidation(t *testing.T) {
	// Checkpoint 5: money-invalid payloads are rejected at the wire boundary
	tests := []struct {
		name     string
		topic    string
		payload  string
		wantErr  bool
		errField string
	}{
		// Valid positive controls
		{
			name: "valid wager",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}`,
			wantErr: false,
		},
		{
			name: "valid settlement",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: false,
		},
		{
			name: "valid refund with no payouts",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k3","winning_outcome":-1,"total":0,"dust":0,"payouts":[],"refunded":true}`,
			wantErr: false,
		},
		// WagerPlaced validation cases
		{
			name: "wager with empty room_id",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}`,
			wantErr: true,
			errField: "room_id",
		},
		{
			name: "wager with empty round_id",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}`,
			wantErr: true,
			errField: "round_id",
		},
		{
			name: "wager with empty user_id",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}`,
			wantErr: true,
			errField: "user_id",
		},
		{
			name: "wager with empty idempotency_key",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"","outcome":1,"amount":50,"balance":950}`,
			wantErr: true,
			errField: "idempotency_key",
		},
		{
			name: "wager with zero amount",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":0,"balance":950}`,
			wantErr: true,
			errField: "amount",
		},
		{
			name: "wager with negative amount",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":-50,"balance":950}`,
			wantErr: true,
			errField: "amount",
		},
		{
			name: "wager with negative outcome",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":-1,"amount":50,"balance":950}`,
			wantErr: true,
			errField: "outcome",
		},
		{
			name: "wager with negative balance",
			topic: TopicWagersPlaced,
			payload: `{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":-1}`,
			wantErr: true,
			errField: "balance",
		},
		// RoundSettled validation cases
		{
			name: "settlement with empty room_id",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "room_id",
		},
		{
			name: "settlement with empty round_id",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "round_id",
		},
		{
			name: "settlement with empty idempotency_key",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "idempotency_key",
		},
		{
			name: "settlement with negative total",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":-1,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "total",
		},
		{
			name: "settlement with negative dust",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":-1,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "dust",
		},
		{
			name: "settlement with zero payout amount",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":0}],"refunded":false}`,
			wantErr: true,
			errField: "amount",
		},
		{
			name: "settlement with empty payout user",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "user_id",
		},
		{
			name: "settlement resolved with no outcome",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":-1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}`,
			wantErr: true,
			errField: "winning_outcome",
		},
		{
			name: "refund carrying an outcome",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k3","winning_outcome":1,"total":100,"dust":0,"payouts":[{"user_id":"u1","amount":100}],"refunded":true}`,
			wantErr: true,
			errField: "winning_outcome",
		},
		{
			name: "refund carrying dust",
			topic: TopicRoundsSettled,
			payload: `{"room_id":"r1","round_id":"rd1","idempotency_key":"k3","winning_outcome":-1,"total":100,"dust":1,"payouts":[{"user_id":"u1","amount":100}],"refunded":true}`,
			wantErr: true,
			errField: "dust",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := DecodeMessage(tt.topic, []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeMessage error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEvent) {
					t.Errorf("DecodeMessage error = %v, want errors.Is(err, ErrInvalidEvent)", err)
				}
				if tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
					t.Errorf("DecodeMessage error %q does not contain field name %q", err.Error(), tt.errField)
				}
			} else {
				if ev == nil {
					t.Errorf("DecodeMessage returned nil event")
				}
			}
		})
	}
}
