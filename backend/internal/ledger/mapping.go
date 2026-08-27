package ledger

import (
	"errors"
	"fmt"

	"github.com/zorojuro12/call_it/backend/internal/events"
)

// Entry is a single debit or credit within a transaction.
type Entry struct {
	Account   AccountRef
	Direction Direction
	Amount    int64
}

// Transaction is a balanced set of ledger entries representing one
// outbox event (a wager, settlement, or refund). IdempotencyKey allows
// the ledger to dedupe Kafka replays: a UNIQUE constraint on this key
// makes at-least-once delivery safe.
type Transaction struct {
	IdempotencyKey string
	Kind           string // "wager" | "settlement" | "refund"
	RoomID         string
	RoundID        string
	Entries        []Entry
}

// ErrUnbalanced is returned when an event's arithmetic is inconsistent
// — the sum of payouts and dust does not equal total.
var ErrUnbalanced = errors.New("ledger: transaction is not balanced")

// TransactionFor maps an event to a balanced Transaction.
// This is a pure function — no I/O, no Redis, no Kafka, no PostgreSQL.
func TransactionFor(ev events.Event) (Transaction, error) {
	switch e := ev.(type) {
	case events.WagerPlaced:
		return transactionForWager(e), nil
	case events.RoundSettled:
		return transactionForSettlement(e)
	default:
		return Transaction{}, fmt.Errorf("%w: unknown event type", events.ErrUnknownEventType)
	}
}

// transactionForWager maps a placed wager to a balanced two-entry transaction:
// - User wallet debit (tokens out of wallet)
// - Round pool credit (tokens into the pool)
func transactionForWager(e events.WagerPlaced) Transaction {
	return Transaction{
		IdempotencyKey: e.IdempotencyKey,
		Kind:           "wager",
		RoomID:         e.RoomID,
		RoundID:        e.RoundID,
		Entries: []Entry{
			{
				Account:   AccountRef{Kind: KindUserWallet, UserID: e.UserID},
				Direction: Debit,
				Amount:    e.Amount,
			},
			{
				Account:   AccountRef{Kind: KindRoundPool, RoomID: e.RoomID},
				Direction: Credit,
				Amount:    e.Amount,
			},
		},
	}
}

// transactionForSettlement maps a settlement or refund to a balanced transaction.
// This implementation handles all settlement variants:
// - CP2: Maps a resolved settlement to pool debit, payout credits, and dust credit
// - CP3: Omits dust entry when Dust is zero (ledger_entries.amount CHECK (amount > 0))
// - CP4: Records refunds with kind "refund" vs "settlement" (Refunded: true)
// - CP5: Handles zero-total settlements (rounds that lock with no wagers)
// - CP6: Verifies arithmetic before building entries (payouts+dust == total)
func transactionForSettlement(e events.RoundSettled) (Transaction, error) {
	// CP6: Verify arithmetic before building the transaction. A violation means
	// the event was corrupted between Redis and this consumer. This is a
	// verification of the event, not a second implementation of the payout formula.
	sum := e.Dust
	for _, p := range e.Payouts {
		sum += p.Amount
	}
	if sum != e.Total {
		return Transaction{}, fmt.Errorf("%w: round %s total %d but payouts+dust %d", ErrUnbalanced, e.RoundID, e.Total, sum)
	}

	// CP4: Kind distinguishes resolved vs refunded in the ledger
	kind := "settlement"
	if e.Refunded {
		kind = "refund"
	}

	var entries []Entry

	// CP2/CP5: Pool debit (tokens leaving the pool). Only added if Total > 0
	// so a round that locks with no wagers produces a zero-entry transaction.
	if e.Total > 0 {
		entries = append(entries, Entry{
			Account:   AccountRef{Kind: KindRoundPool, RoomID: e.RoomID},
			Direction: Debit,
			Amount:    e.Total,
		})
	}

	// CP2: Payout credits (tokens into user wallets), in event order
	for _, payout := range e.Payouts {
		entries = append(entries, Entry{
			Account:   AccountRef{Kind: KindUserWallet, UserID: payout.UserID},
			Direction: Credit,
			Amount:    payout.Amount,
		})
	}

	// CP2/CP3: Dust credit (rounding remainder to system).
	// CP3: Only added if Dust > 0. Zero dust is common (exactly-divisible rounds)
	// and would violate ledger_entries.amount CHECK (amount > 0) constraint.
	if e.Dust > 0 {
		entries = append(entries, Entry{
			Account:   AccountRef{Kind: KindSystemDust},
			Direction: Credit,
			Amount:    e.Dust,
		})
	}

	return Transaction{
		IdempotencyKey: e.IdempotencyKey,
		Kind:           kind,
		RoomID:         e.RoomID,
		RoundID:        e.RoundID,
		Entries:        entries,
	}, nil
}
