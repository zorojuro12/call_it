package relay

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
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
