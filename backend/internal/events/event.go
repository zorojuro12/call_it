// Package events defines the typed schema over wager-outbox entries and
// decodes them from the raw Redis Stream field map. No Redis, Kafka, or
// PostgreSQL import here — every type and decode path is pure.
package events

const (
	// TopicWagersPlaced is the Kafka topic every WagerPlaced event routes to.
	TopicWagersPlaced = "wagers-placed"
	// TopicRoundsSettled is the Kafka topic both RoundSettled and its
	// refund variant route to — a refund is a settlement outcome, and
	// routing it elsewhere would break the per-room ordering guarantee
	// that settlements must follow the wagers they settle.
	TopicRoundsSettled = "rounds-settled"
)

// Event is the common interface every decoded outbox entry satisfies,
// giving cmd/relay enough to route and dedupe without knowing the
// concrete event type.
type Event interface {
	// Topic is the Kafka topic this event routes to.
	Topic() string
	// PartitionKey is the Kafka partition key — parent plan §7 keys by
	// room_id, so per-room ordering holds regardless of partition count.
	PartitionKey() string
	// Key returns the idempotency key already minted on the wager path.
	// A relay must never mint a second one (CLAUDE.md) — at-least-once
	// Kafka delivery is made safe downstream by this key's UNIQUE
	// constraint on the ledger's transactions table, and a second
	// identity path would defeat it.
	Key() string
}

// Payout is one credit produced by settling or refunding a round: the
// outbox wire format for domain.Payout, kept separate so the domain type
// never grows JSON tags (Amendment E1).
type Payout struct {
	UserID string `json:"user_id"`
	Amount int64  `json:"amount"`
}

// WagerPlaced is a single wager hitting a round's pool.
// This is the Kafka wire format — renaming a field changes the wire protocol.
// Do not reorder fields; Go marshals in declaration order and tests pin it.
type WagerPlaced struct {
	RoomID         string `json:"room_id"`
	RoundID        string `json:"round_id"`
	UserID         string `json:"user_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Outcome        int    `json:"outcome"`
	Amount         int64  `json:"amount"`
	Balance        int64  `json:"balance"`
}

func (e WagerPlaced) Topic() string        { return TopicWagersPlaced }
func (e WagerPlaced) PartitionKey() string { return e.RoomID }
func (e WagerPlaced) Key() string          { return e.IdempotencyKey }

// RoundSettled serves both terminal event types the outbox produces —
// round_settled and round_refunded — distinguished by Refunded. They
// carry identical fields and produce identical ledger shapes in Phase
// 5b, so two structs would be two copies of one thing.
// This is the Kafka wire format — renaming a field changes the wire protocol.
// Do not reorder fields; Go marshals in declaration order and tests pin it.
type RoundSettled struct {
	RoomID         string   `json:"room_id"`
	RoundID        string   `json:"round_id"`
	IdempotencyKey string   `json:"idempotency_key"`
	// WinningOutcome is -1 for a refund. 0 is a valid outcome index and
	// would be indistinguishable from a real outcome-0 win, so -1 is the
	// sentinel for "no winning outcome" rather than the empty value.
	WinningOutcome int      `json:"winning_outcome"`
	Total          int64    `json:"total"`
	Dust           int64    `json:"dust"`
	Payouts        []Payout `json:"payouts"`
	Refunded       bool     `json:"refunded"`
}

func (e RoundSettled) Topic() string        { return TopicRoundsSettled }
func (e RoundSettled) PartitionKey() string { return e.RoomID }
func (e RoundSettled) Key() string          { return e.IdempotencyKey }
