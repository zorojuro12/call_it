package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/events"
)

func TestEnsureGroupIsIdempotent(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", nil)

	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() first call = %v, want nil", err)
	}
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() second call = %v, want nil (must swallow BUSYGROUP)", err)
	}

	groups, err := testClient.XInfoGroups(ctx, stream).Result()
	if err != nil {
		t.Fatalf("XINFO GROUPS: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("XINFO GROUPS returned %d groups, want 1", len(groups))
	}
	if groups[0].Name != group {
		t.Errorf("group name = %q, want %q", groups[0].Name, group)
	}
}

func xaddWagerPlaced(t *testing.T, ctx context.Context, stream string) string {
	t.Helper()
	id, err := testClient.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"type": "wager_placed", "user": "u1", "outcome": "0",
			"amount": "100", "balance": "900",
			"idempotency_key": "idem-w1", "room_id": "r1", "round_id": "rd1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("XADD wager_placed: %v", err)
	}
	return id
}

func xaddRoundSettled(t *testing.T, ctx context.Context, stream string) string {
	t.Helper()
	id, err := testClient.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"type": "round_settled", "round_id": "rd1", "room_id": "r1",
			"dust": "0", "total": "100", "winning_outcome": "0",
			"payouts": `[{"user_id":"u1","amount":100}]`, "idempotency_key": "idem-s1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("XADD round_settled: %v", err)
	}
	return id
}

func xaddRoundRefunded(t *testing.T, ctx context.Context, stream string) string {
	t.Helper()
	id, err := testClient.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"type": "round_refunded", "round_id": "rd2", "room_id": "r2",
			"dust": "0", "total": "50", "winning_outcome": "",
			"payouts": `[{"user_id":"u2","amount":50}]`, "idempotency_key": "idem-r1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("XADD round_refunded: %v", err)
	}
	return id
}

func TestOnceProducesThenAcks(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", nil)
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() = %v, want nil", err)
	}

	xaddWagerPlaced(t, ctx, stream)
	xaddRoundSettled(t, ctx, stream)
	xaddRoundRefunded(t, ctx, stream)

	fake := &fakeProducer{}
	r.producer = fake

	relayed, err := r.Once(ctx, 10, time.Second)
	if err != nil {
		t.Fatalf("Once() error = %v, want nil", err)
	}
	if relayed != 3 {
		t.Errorf("Once() relayed = %d, want 3", relayed)
	}

	if fake.callCount() != 1 {
		t.Errorf("Produce called %d times, want 1 (must batch, not call per entry)", fake.callCount())
	}
	if len(fake.lastBatch()) != 3 {
		t.Errorf("Produce batch size = %d, want 3", len(fake.lastBatch()))
	}

	pending, err := testClient.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 0 {
		t.Errorf("XPENDING count = %d, want 0 (all three must be acked)", pending.Count)
	}
}

func TestOnceDoesNotAckOnProduceFailure(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", nil)
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() = %v, want nil", err)
	}

	xaddWagerPlaced(t, ctx, stream)
	xaddRoundSettled(t, ctx, stream)

	failing := &fakeProducer{err: errors.New("broker down")}
	r.producer = failing

	if _, err := r.Once(ctx, 10, time.Second); err == nil {
		t.Fatalf("Once() error = nil, want an error wrapping the producer's failure")
	}

	pending, err := testClient.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 2 {
		t.Errorf("XPENDING count = %d, want 2 (nothing acked on produce failure)", pending.Count)
	}

	succeeding := &fakeProducer{}
	r.producer = succeeding

	relayed, err := r.Recover(ctx, 10)
	if err != nil {
		t.Fatalf("Recover() error = %v, want nil", err)
	}
	if relayed != 2 {
		t.Errorf("Recover() relayed = %d, want 2", relayed)
	}
	if succeeding.callCount() != 1 {
		t.Errorf("Produce called %d times during Recover, want 1", succeeding.callCount())
	}

	pendingAfter, err := testClient.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("XPENDING after recover: %v", err)
	}
	if pendingAfter.Count != 0 {
		t.Errorf("XPENDING count after Recover = %d, want 0", pendingAfter.Count)
	}
}

func TestOnceHaltsOnUndecodableEntry(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", nil)
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() = %v, want nil", err)
	}

	xaddWagerPlaced(t, ctx, stream)
	if _, err := testClient.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{"type": "garbage"},
	}).Result(); err != nil {
		t.Fatalf("XADD garbage entry: %v", err)
	}

	fake := &fakeProducer{}
	r.producer = fake

	_, err := r.Once(ctx, 10, time.Second)
	if !errors.Is(err, events.ErrUnknownEventType) {
		t.Fatalf("Once() error = %v, want errors.Is(err, events.ErrUnknownEventType)", err)
	}

	if fake.callCount() != 0 {
		t.Errorf("Produce called %d times, want 0 (nothing produced when a batch member fails to decode)", fake.callCount())
	}

	pending, err := testClient.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 2 {
		t.Errorf("XPENDING count = %d, want 2 (nothing acked on decode failure)", pending.Count)
	}
}

func TestRunRecoversBeforeNewWork(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	setup := New(testClient, stream, group, "consumer-1", nil)
	if err := setup.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() = %v, want nil", err)
	}

	// Leave one entry pending: read it with a failing producer so it is
	// never acked.
	xaddWagerPlaced(t, ctx, stream) // the older, recovered entry
	setup.producer = &fakeProducer{err: errors.New("broker down")}
	if _, err := setup.Once(ctx, 10, time.Second); err == nil {
		t.Fatalf("Once() with failing producer = nil error, want an error")
	}

	// A second, newer entry arrives after the first is already pending.
	xaddRoundSettled(t, ctx, stream)

	runCtx, cancel := context.WithCancel(ctx)
	var recorded []events.Event
	var mu sync.Mutex
	recording := producerFunc(func(_ context.Context, evs []events.Event) error {
		mu.Lock()
		recorded = append(recorded, evs...)
		n := len(recorded)
		mu.Unlock()
		if n >= 2 {
			cancel()
		}
		return nil
	})

	r := New(testClient, stream, group, "consumer-1", recording)

	done := make(chan error, 1)
	go func() { done <- r.Run(runCtx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil (context cancellation is a clean shutdown)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within 5s of the context being cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(recorded) < 2 {
		t.Fatalf("recorded %d events, want at least 2", len(recorded))
	}
	first, ok := recorded[0].(events.WagerPlaced)
	if !ok {
		t.Fatalf("first recorded event = %T, want events.WagerPlaced (the recovered pending entry)", recorded[0])
	}
	if first.IdempotencyKey != "idem-w1" {
		t.Errorf("first recorded event idempotency key = %q, want %q", first.IdempotencyKey, "idem-w1")
	}
	second, ok := recorded[1].(events.RoundSettled)
	if !ok {
		t.Fatalf("second recorded event = %T, want events.RoundSettled (the new entry, read only after recovery)", recorded[1])
	}
	if second.IdempotencyKey != "idem-s1" {
		t.Errorf("second recorded event idempotency key = %q, want %q", second.IdempotencyKey, "idem-s1")
	}
}
