package redisstore

import (
	"context"
	"reflect"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func TestReadStakes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roundID := testID(t, "round")

	if err := store.client.HSet(ctx, RoundWagersKey(roundID),
		WagerField("u2", 1), "300",
		WagerField("u1", 0), "100",
		WagerField("u1", 2), "50",
		WagerField("u3", 1), "75",
	).Err(); err != nil {
		t.Fatalf("HSET wagers: %v", err)
	}

	stakes, err := store.ReadStakes(ctx, roundID)
	if err != nil {
		t.Fatalf("ReadStakes() = %v, want nil", err)
	}

	want := []domain.Stake{
		{UserID: "u1", Outcome: 0, Amount: 100},
		{UserID: "u1", Outcome: 2, Amount: 50},
		{UserID: "u2", Outcome: 1, Amount: 300},
		{UserID: "u3", Outcome: 1, Amount: 75},
	}
	if !reflect.DeepEqual(stakes, want) {
		t.Errorf("ReadStakes() = %+v, want %+v", stakes, want)
	}

	emptyRoundID := testID(t, "round")
	empty, err := store.ReadStakes(ctx, emptyRoundID)
	if err != nil {
		t.Fatalf("ReadStakes() on empty round = %v, want nil", err)
	}
	if empty == nil {
		t.Errorf("ReadStakes() on empty round = nil, want non-nil empty slice")
	}
	if len(empty) != 0 {
		t.Errorf("ReadStakes() on empty round = %v, want empty", empty)
	}

	malformedRoundID := testID(t, "round")
	if err := store.client.HSet(ctx, RoundWagersKey(malformedRoundID), "garbage", "10").Err(); err != nil {
		t.Fatalf("HSET malformed wager: %v", err)
	}
	if _, err := store.ReadStakes(ctx, malformedRoundID); err == nil {
		t.Errorf("ReadStakes() on malformed field = nil error, want an error naming the field")
	}
}
