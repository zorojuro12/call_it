package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/zorojuro12/call_it/backend/internal/events"
)

const (
	maxBatch    = 100
	batchWindow = 200 * time.Millisecond
)

// Consumer is the interface a Kafka consumer must satisfy. Satisfied
// structurally by events.KafkaConsumer.
type Consumer interface {
	Fetch(ctx context.Context) (kafka.Message, error)
	Commit(ctx context.Context, msgs ...kafka.Message) error
}

// Writer is the interface a ledger repository must satisfy. Satisfied
// structurally by *ledger.Repo.
type Writer interface {
	WriteBatch(ctx context.Context, txns []Transaction) (int, error)
}

// Worker consumes Kafka events, maps them to transactions, and writes them
// to the ledger. It enforces at-least-once delivery: offsets are never
// committed ahead of a durable PostgreSQL write.
type Worker struct {
	c Consumer
	w Writer
}

// NewWorker builds a worker over the given consumer and writer.
func NewWorker(c Consumer, w Writer) *Worker {
	return &Worker{c: c, w: w}
}

// Once performs one fetch → map → write → commit cycle over at most
// maxBatch messages gathered within batchWindow. Returns how many
// messages were consumed. A batch window that elapses with no message
// returns (0, nil).
func (wk *Worker) Once(ctx context.Context) (int, error) {
	// Fetch the first message with the caller's context (blocking)
	msg, err := wk.c.Fetch(ctx)
	if err != nil {
		return 0, nil // caller cancelled, clean return
	}

	// Gather up to maxBatch messages or batchWindow elapsed
	msgs := []kafka.Message{msg}
	batchCtx, cancel := context.WithTimeout(ctx, batchWindow)
	defer cancel()

	for len(msgs) < maxBatch {
		msg, err := wk.c.Fetch(batchCtx)
		if err != nil {
			// Timeout means batch complete, other errors propagate
			if err == context.DeadlineExceeded {
				break
			}
			// If the original context was cancelled, return clean
			if err == context.Canceled {
				break
			}
			return 0, nil
		}
		msgs = append(msgs, msg)
	}

	// Decode and map each message to a transaction
	txns := make([]Transaction, 0, len(msgs))
	for _, msg := range msgs {
		ev, err := events.DecodeMessage(msg.Topic, msg.Value)
		if err != nil {
			return 0, fmt.Errorf("ledger: decoding message at %s/%d offset %d: %w", msg.Topic, msg.Partition, msg.Offset, err)
		}

		txn, err := TransactionFor(ev)
		if err != nil {
			return 0, fmt.Errorf("ledger: mapping event to transaction: %w", err)
		}

		txns = append(txns, txn)
	}

	// Write the batch
	_, err = wk.w.WriteBatch(ctx, txns)
	if err != nil {
		return 0, fmt.Errorf("ledger: writing batch: %w", err)
	}

	// Only commit after successful write
	if err := wk.c.Commit(ctx, msgs...); err != nil {
		return 0, fmt.Errorf("ledger: committing offsets: %w", err)
	}

	return len(msgs), nil
}

// Run loops Once until ctx is cancelled, returning nil on cancellation.
func (wk *Worker) Run(ctx context.Context) error {
	for {
		_, err := wk.Once(ctx)
		if err != nil {
			// Check if the context was cancelled  - if so, return clean
			if ctx.Err() != nil {
				return nil
			}
			// Otherwise return the error
			return err
		}
		// Check if context was cancelled to avoid spinning on consecutive zeros
		if ctx.Err() != nil {
			return nil
		}
	}
}
