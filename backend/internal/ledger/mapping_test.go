package ledger

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/events"
)

// testUnknownEvent is a stub type that satisfies events.Event but
// is not WagerPlaced or RoundSettled, for testing unknown event handling.
type testUnknownEvent struct{}

func (*testUnknownEvent) Topic() string        { return "unknown" }
func (*testUnknownEvent) PartitionKey() string { return "" }
func (*testUnknownEvent) Key() string          { return "" }

// TestTransactionForWager tests that a placed wager maps to a balanced
// two-entry transaction: user wallet debit, round pool credit.
func TestTransactionForWager(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	userID := uuid.NewString()

	wager := events.WagerPlaced{
		RoomID:         roomID,
		RoundID:        roundID,
		UserID:         userID,
		IdempotencyKey: "k1",
		Outcome:        1,
		Amount:         50,
		Balance:        950,
	}

	txn, err := TransactionFor(wager)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := Transaction{
		IdempotencyKey: "k1",
		Kind:           "wager",
		RoomID:         roomID,
		RoundID:        roundID,
		Entries: []Entry{
			{
				Account:   AccountRef{Kind: KindUserWallet, UserID: userID},
				Direction: Debit,
				Amount:    50,
			},
			{
				Account:   AccountRef{Kind: KindRoundPool, RoomID: roomID},
				Direction: Credit,
				Amount:    50,
			},
		},
	}

	if !reflect.DeepEqual(txn, expected) {
		t.Errorf("transaction mismatch:\nexpected: %#v\ngot:      %#v", expected, txn)
	}

	// Test that AccountRef.ID() is stable and kind-dependent
	id1 := AccountRef{Kind: KindUserWallet, UserID: "u"}.ID()
	id2 := AccountRef{Kind: KindUserWallet, UserID: "u"}.ID()
	if id1 != id2 {
		t.Errorf("AccountRef.ID() not stable: %v != %v", id1, id2)
	}

	id3 := AccountRef{Kind: KindRoundPool, RoomID: "u"}.ID()
	if id1 == id3 {
		t.Errorf("different account kinds collided: %v == %v", id1, id3)
	}
}

// TestTransactionForSettlement tests that a resolved settlement maps to a
// transaction with pool debit, payout credits, and dust credit.
func TestTransactionForSettlement(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	uA := uuid.NewString()
	uB := uuid.NewString()

	settled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "k2",
		WinningOutcome: 1,
		Total:          100,
		Dust:           2,
		Payouts: []events.Payout{
			{UserID: uA, Amount: 60},
			{UserID: uB, Amount: 38},
		},
		Refunded: false,
	}

	txn, err := TransactionFor(settled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := Transaction{
		IdempotencyKey: "k2",
		Kind:           "settlement",
		RoomID:         roomID,
		RoundID:        roundID,
		Entries: []Entry{
			{
				Account:   AccountRef{Kind: KindRoundPool, RoomID: roomID},
				Direction: Debit,
				Amount:    100,
			},
			{
				Account:   AccountRef{Kind: KindUserWallet, UserID: uA},
				Direction: Credit,
				Amount:    60,
			},
			{
				Account:   AccountRef{Kind: KindUserWallet, UserID: uB},
				Direction: Credit,
				Amount:    38,
			},
			{
				Account:   AccountRef{Kind: KindSystemDust},
				Direction: Credit,
				Amount:    2,
			},
		},
	}

	if !reflect.DeepEqual(txn, expected) {
		t.Errorf("transaction mismatch:\nexpected: %#v\ngot:      %#v", expected, txn)
	}
}

// TestTransactionForZeroDust tests that a settlement with zero dust
// produces no dust entry.
func TestTransactionForZeroDust(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	uA := uuid.NewString()
	uB := uuid.NewString()

	settled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "k2",
		WinningOutcome: 1,
		Total:          100,
		Dust:           0,
		Payouts: []events.Payout{
			{UserID: uA, Amount: 60},
			{UserID: uB, Amount: 40},
		},
		Refunded: false,
	}

	txn, err := TransactionFor(settled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have exactly 3 entries: pool debit, two payouts
	if len(txn.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(txn.Entries))
	}

	// Check that there's no dust entry
	for _, entry := range txn.Entries {
		if entry.Account.Kind == KindSystemDust {
			t.Errorf("unexpected dust entry with amount 0")
		}
	}
}

// TestTransactionForRefund tests that a refund is recorded with kind "refund".
func TestTransactionForRefund(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	uA := uuid.NewString()
	uB := uuid.NewString()

	settled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "k3",
		WinningOutcome: -1,
		Total:          100,
		Dust:           0,
		Payouts: []events.Payout{
			{UserID: uA, Amount: 60},
			{UserID: uB, Amount: 40},
		},
		Refunded: true,
	}

	txn, err := TransactionFor(settled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if txn.Kind != "refund" {
		t.Errorf("expected kind 'refund', got %q", txn.Kind)
	}

	if len(txn.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(txn.Entries))
	}
}

// TestTransactionForEmptyRound tests that a zero-total settlement with
// no wagers maps to an empty transaction.
func TestTransactionForEmptyRound(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()

	settled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "k4",
		WinningOutcome: -1,
		Total:          0,
		Dust:           0,
		Payouts:        []events.Payout{},
		Refunded:       true,
	}

	txn, err := TransactionFor(settled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if txn.Kind != "refund" {
		t.Errorf("expected kind 'refund', got %q", txn.Kind)
	}

	if len(txn.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(txn.Entries))
	}
}

// TestTransactionForUnbalanced tests that unbalanced settlements are rejected.
func TestTransactionForUnbalanced(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	uA := uuid.NewString()
	uB := uuid.NewString()

	tests := []struct {
		name    string
		settled events.RoundSettled
	}{
		{
			name: "payouts+dust less than total",
			settled: events.RoundSettled{
				RoomID:         roomID,
				RoundID:        roundID,
				IdempotencyKey: "k5",
				WinningOutcome: 1,
				Total:          100,
				Dust:           2,
				Payouts: []events.Payout{
					{UserID: uA, Amount: 60},
				},
				Refunded: false,
			},
		},
		{
			name: "payouts exceed total",
			settled: events.RoundSettled{
				RoomID:         roomID,
				RoundID:        roundID,
				IdempotencyKey: "k6",
				WinningOutcome: 1,
				Total:          100,
				Dust:           0,
				Payouts: []events.Payout{
					{UserID: uA, Amount: 60},
					{UserID: uB, Amount: 50},
				},
				Refunded: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn, err := TransactionFor(tt.settled)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrUnbalanced) {
				t.Errorf("expected ErrUnbalanced, got %v", err)
			}
			if txn.Kind != "" || len(txn.Entries) != 0 {
				t.Errorf("expected zero transaction on error, got %#v", txn)
			}
		})
	}

	// Positive control: valid settlement
	validSettled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "k7",
		WinningOutcome: 1,
		Total:          100,
		Dust:           2,
		Payouts: []events.Payout{
			{UserID: uA, Amount: 60},
			{UserID: uB, Amount: 38},
		},
		Refunded: false,
	}
	txn, err := TransactionFor(validSettled)
	if err != nil {
		t.Fatalf("valid settlement should not error: %v", err)
	}
	if txn.Kind != "settlement" {
		t.Errorf("expected settlement, got %q", txn.Kind)
	}

	// Test unknown event type
	_, err = TransactionFor(&testUnknownEvent{})
	if !errors.Is(err, events.ErrUnknownEventType) {
		t.Errorf("expected ErrUnknownEventType for unknown event, got %v", err)
	}
}
