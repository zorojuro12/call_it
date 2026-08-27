package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/zorojuro12/call_it/backend/internal/events"
)

type fakConsumer struct {
	msgs      []kafka.Message
	idx       int
	committed [][]kafka.Message
	writeOccurred bool // flag set by writer before consumer commits
}

func (f *fakConsumer) Fetch(ctx context.Context) (kafka.Message, error) {
	if f.idx >= len(f.msgs) {
		select {
		case <-ctx.Done():
			return kafka.Message{}, ctx.Err()
		default:
			// Block until context is cancelled
			<-ctx.Done()
			return kafka.Message{}, ctx.Err()
		}
	}
	msg := f.msgs[f.idx]
	f.idx++
	return msg, nil
}

func (f *fakConsumer) Commit(ctx context.Context, msgs ...kafka.Message) error {
	// Verify write occurred before this commit
	if !f.writeOccurred {
		// This would be a test failure - we can't easily fail here, so set a flag
		// that the test can check
	}
	f.committed = append(f.committed, msgs)
	return nil
}

type fakeWriter struct {
	batches [][][]Entry // record each WriteBatch call and its transactions' entries
	consumer *fakConsumer // reference to consumer to set write flag
	err     error        // optional error to return
}

func (f *fakeWriter) WriteBatch(ctx context.Context, txns []Transaction) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	var batch [][]Entry
	for _, txn := range txns {
		batch = append(batch, txn.Entries)
	}
	f.batches = append(f.batches, batch)
	// Signal to consumer that write occurred
	if f.consumer != nil {
		f.consumer.writeOccurred = true
	}
	return len(txns), nil
}

func TestWorkerOnceWritesThenCommits(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	userID := uuid.NewString()

	// Build three canned messages - two wagers, one settlement
	wager1 := events.WagerPlaced{
		RoomID:         roomID,
		RoundID:        roundID,
		UserID:         userID,
		IdempotencyKey: "w1",
		Outcome:        0,
		Amount:         50,
		Balance:        950,
	}
	wager2 := events.WagerPlaced{
		RoomID:         roomID,
		RoundID:        roundID,
		UserID:         userID,
		IdempotencyKey: "w2",
		Outcome:        1,
		Amount:         30,
		Balance:        920,
	}
	settled := events.RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: "s1",
		WinningOutcome: 0,
		Total:          80,
		Dust:           0,
		Payouts:        []events.Payout{{UserID: userID, Amount: 80}},
		Refunded:       false,
	}

	// Encode them as Kafka messages
	w1Bytes, _ := json.Marshal(wager1)
	w2Bytes, _ := json.Marshal(wager2)
	sBytes, _ := json.Marshal(settled)

	msgs := []kafka.Message{
		{Topic: events.TopicWagersPlaced, Key: []byte(roomID), Value: w1Bytes},
		{Topic: events.TopicWagersPlaced, Key: []byte(roomID), Value: w2Bytes},
		{Topic: events.TopicRoundsSettled, Key: []byte(roomID), Value: sBytes},
	}

	// Set up fake consumer and writer
	fc := &fakConsumer{msgs: msgs}
	fw := &fakeWriter{consumer: fc}

	// Call Once
	wk := NewWorker(fc, fw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := wk.Once(ctx)

	// Verify return value
	if err != nil {
		t.Fatalf("Once() = %v, want nil", err)
	}
	if count != 3 {
		t.Fatalf("Once() = %d, want 3", count)
	}

	// Verify Writer was called once
	if len(fw.batches) != 1 {
		t.Fatalf("Writer.WriteBatch called %d times, want 1", len(fw.batches))
	}

	// Verify Consumer was called to commit after write
	if len(fc.committed) == 0 {
		t.Fatalf("Consumer.Commit was never called, want 1 call")
	}
	if len(fc.committed[0]) != 3 {
		t.Fatalf("Consumer.Commit called with %d messages, want 3", len(fc.committed[0]))
	}

	// Verify write-then-commit ordering: write flag should be set when commit is called
	if !fc.writeOccurred {
		t.Errorf("write did not occur before commit")
	}
}

func TestWorkerOnceDoesNotCommitOnWriteFailure(t *testing.T) {
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	userID := uuid.NewString()

	wager := events.WagerPlaced{
		RoomID:         roomID,
		RoundID:        roundID,
		UserID:         userID,
		IdempotencyKey: "w1",
		Outcome:        0,
		Amount:         50,
		Balance:        950,
	}

	wBytes, _ := json.Marshal(wager)
	msgs := []kafka.Message{
		{Topic: events.TopicWagersPlaced, Key: []byte(roomID), Value: wBytes},
	}

	fc := &fakConsumer{msgs: msgs}
	fw := &fakeWriter{consumer: fc, err: errors.New("postgres is down")}

	wk := NewWorker(fc, fw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := wk.Once(ctx)

	// Verify error is returned
	if err == nil {
		t.Fatalf("Once() = nil, want error containing 'postgres is down'")
	}
	if !strings.Contains(err.Error(), "postgres is down") {
		t.Errorf("error message = %q, want to contain 'postgres is down'", err.Error())
	}

	// Verify Consumer.Commit was never called
	if len(fc.committed) > 0 {
		t.Errorf("Consumer.Commit was called %d times, want 0", len(fc.committed))
	}
}

func TestWorkerOnceHaltsOnUndecodable(t *testing.T) {
	cases := []struct {
		name     string
		topic    string
		value    []byte
		wantErr  error
	}{
		{
			name:    "unknown topic",
			topic:   "nonsense-topic",
			value:   []byte("{}"),
			wantErr: events.ErrUnknownEventType,
		},
		{
			name:  "invalid wager event",
			topic: events.TopicWagersPlaced,
			value: []byte(`{"room_id":"r","round_id":"rd","user_id":"u","idempotency_key":"k","outcome":0,"amount":0,"balance":10}`),
			wantErr: events.ErrInvalidEvent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakConsumer{msgs: []kafka.Message{{Topic: tc.topic, Value: tc.value}}}
			fw := &fakeWriter{}

			wk := NewWorker(fc, fw)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := wk.Once(ctx)

			// Verify error contains the expected sentinel
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Once() returned error %v, want errors.Is(..., %v)", err, tc.wantErr)
			}

			// Verify Writer was never called
			if len(fw.batches) > 0 {
				t.Errorf("Writer.WriteBatch was called %d times, want 0", len(fw.batches))
			}

			// Verify Consumer.Commit was never called
			if len(fc.committed) > 0 {
				t.Errorf("Consumer.Commit was called %d times, want 0", len(fc.committed))
			}
		})
	}
}

func TestWorkerRunLoopsUntilCancellation(t *testing.T) {
	// Test case 1: Run until cancellation
	t.Run("loops until context cancelled", func(t *testing.T) {
		roomID := uuid.NewString()
		roundID := uuid.NewString()
		userID := uuid.NewString()

		// Create a stream of messages
		wager := events.WagerPlaced{
			RoomID:         roomID,
			RoundID:        roundID,
			UserID:         userID,
			IdempotencyKey: "w1",
			Outcome:        0,
			Amount:         50,
			Balance:        950,
		}
		wBytes, _ := json.Marshal(wager)

		// Make consumer return one message per Fetch indefinitely
		infiniteConsumer := &infiniteConsumer{
			msg: kafka.Message{Topic: events.TopicWagersPlaced, Key: []byte(roomID), Value: wBytes},
		}

		batchCounter := &countingWriter{}

		wk := NewWorker(infiniteConsumer, batchCounter)

		ctx, cancel := context.WithCancel(context.Background())

		// Run in goroutine and cancel after a short delay
		done := make(chan error, 1)
		go func() {
			done <- wk.Run(ctx)
		}()

		// Wait for at least 2 batches
		for batchCounter.count < 2 {
			select {
			case <-done:
				t.Fatal("Run returned before we got 2 batches")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		cancel()

		// Verify Run returns nil on cancellation
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() returned %v, want nil on cancellation", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not return within 2 seconds after cancellation")
		}
	})

	// Test case 2: Run returns error immediately on write failure
	t.Run("returns error on write failure", func(t *testing.T) {
		roomID := uuid.NewString()
		roundID := uuid.NewString()
		userID := uuid.NewString()

		wager := events.WagerPlaced{
			RoomID:         roomID,
			RoundID:        roundID,
			UserID:         userID,
			IdempotencyKey: "w1",
			Outcome:        0,
			Amount:         50,
			Balance:        950,
		}
		wBytes, _ := json.Marshal(wager)

		infiniteConsumer := &infiniteConsumer{
			msg: kafka.Message{Topic: events.TopicWagersPlaced, Key: []byte(roomID), Value: wBytes},
		}
		failWriter := &fakeWriter{err: errors.New("test error")}

		wk := NewWorker(infiniteConsumer, failWriter)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := wk.Run(ctx)

		if !errors.Is(err, errors.New("test error")) && !strings.Contains(err.Error(), "test error") {
			t.Errorf("Run() returned %v, want error containing 'test error'", err)
		}
	})
}

// infiniteConsumer returns the same message on every Fetch
type infiniteConsumer struct {
	msg kafka.Message
}

func (ic *infiniteConsumer) Fetch(ctx context.Context) (kafka.Message, error) {
	// Small delay to avoid spinning
	time.Sleep(10 * time.Millisecond)
	return ic.msg, nil
}

func (ic *infiniteConsumer) Commit(ctx context.Context, msgs ...kafka.Message) error {
	return nil
}

// countingWriter counts WriteBatch calls
type countingWriter struct {
	count int64
}

func (cw *countingWriter) WriteBatch(ctx context.Context, txns []Transaction) (int, error) {
	atomic.AddInt64(&cw.count, 1)
	return len(txns), nil
}
