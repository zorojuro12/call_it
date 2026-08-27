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
// Refunds are identified by Refunded: true.
// A settlement with no wagers (Total: 0) produces a zero-entry transaction.
// The dust entry is omitted when Dust is zero.
func transactionForSettlement(e events.RoundSettled) (Transaction, error) {
	// Verify arithmetic before building the transaction
	sum := e.Dust
	for _, p := range e.Payouts {
		sum += p.Amount
	}
	if sum != e.Total {
		return Transaction{}, fmt.Errorf("%w: round %s total %d but payouts+dust %d", ErrUnbalanced, e.RoundID, e.Total, sum)
	}

	kind := "settlement"
	if e.Refunded {
		kind = "refund"
	}

	var entries []Entry

	// Pool debit (tokens leaving the pool)
	if e.Total > 0 {
		entries = append(entries, Entry{
			Account:   AccountRef{Kind: KindRoundPool, RoomID: e.RoomID},
			Direction: Debit,
			Amount:    e.Total,
		})
	}

	// Payout credits (tokens into user wallets)
	for _, payout := range e.Payouts {
		entries = append(entries, Entry{
			Account:   AccountRef{Kind: KindUserWallet, UserID: payout.UserID},
			Direction: Credit,
			Amount:    payout.Amount,
		})
	}

	// Dust credit (rounding remainder to system)
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
