package redisstore

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

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

// setupSettleRound creates a room and a locked round with the given
// players' stakes already placed, returning the room and round IDs.
// Each entry in stakes is one wager: {userID, outcome, amount}.
func setupSettleRound(t *testing.T, store *Store, hostID string, buyIn domain.Tokens, outcomeCount int, players map[string]domain.Tokens, stakes []domain.Stake) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()

	roomID = testID(t, "room")
	roundID = testID(t, "round")
	lockAt := time.Now().Add(30 * time.Second)

	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), hostID, buyIn); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if err := store.CreateRound(ctx, roundID, roomID, outcomeCount, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}
	for userID, balance := range players {
		if err := store.JoinRoom(ctx, roomID, userID, balance); err != nil {
			t.Fatalf("JoinRoom(%s) = %v, want nil", userID, err)
		}
	}
	for _, stake := range stakes {
		if _, err := store.PlaceWager(ctx, WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: stake.UserID,
			Outcome: stake.Outcome, Amount: stake.Amount, IdempotencyKey: testID(t, "idem"),
		}); err != nil {
			t.Fatalf("PlaceWager(%s, outcome %d, %d) = %v, want nil", stake.UserID, stake.Outcome, stake.Amount, err)
		}
	}
	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() = %v, want nil", err)
	}

	return roomID, roundID
}

func TestSettleRound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	roomID, roundID := setupSettleRound(t, store, "host1", 500, 3,
		map[string]domain.Tokens{"u1": 500, "u2": 500, "u3": 500},
		[]domain.Stake{
			{UserID: "u1", Outcome: 0, Amount: 100},
			{UserID: "u2", Outcome: 1, Amount: 300},
			{UserID: "u3", Outcome: 0, Amount: 100},
		})

	before, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox before: %v", err)
	}

	settlement, err := store.SettleRound(ctx, roundID, 0, testID(t, "idem"))
	if err != nil {
		t.Fatalf("SettleRound() = %v, want nil", err)
	}

	wantPayouts := []domain.Payout{{UserID: "u1", Amount: 250}, {UserID: "u3", Amount: 250}}
	if !reflect.DeepEqual(settlement.Payouts, wantPayouts) {
		t.Errorf("Payouts = %+v, want %+v", settlement.Payouts, wantPayouts)
	}
	if settlement.Dust != 0 {
		t.Errorf("Dust = %d, want 0", settlement.Dust)
	}
	if settlement.Refunded {
		t.Errorf("Refunded = true, want false")
	}
	wantNet := map[string]domain.Tokens{"u1": 150, "u2": -300, "u3": 150}
	for _, r := range settlement.Results {
		if r.Net != wantNet[r.UserID] {
			t.Errorf("Results[%s].Net = %d, want %d", r.UserID, r.Net, wantNet[r.UserID])
		}
	}

	for userID, want := range map[string]string{"u1": "650", "u2": "200", "u3": "650"} {
		got, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
		if err != nil {
			t.Fatalf("HGET wallets %s: %v", userID, err)
		}
		if got != want {
			t.Errorf("HGET wallets %s = %q, want %q", userID, got, want)
		}
	}

	status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
	if err != nil {
		t.Fatalf("HGET round status: %v", err)
	}
	if status != "resolved" {
		t.Errorf("status = %q, want %q", status, "resolved")
	}
	resolvedOutcome, err := store.client.HGet(ctx, RoundKey(roundID), "resolved_outcome").Result()
	if err != nil {
		t.Fatalf("HGET resolved_outcome: %v", err)
	}
	if resolvedOutcome != "0" {
		t.Errorf("resolved_outcome = %q, want %q", resolvedOutcome, "0")
	}

	after, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox after: %v", err)
	}
	if after != before+1 {
		t.Errorf("XLEN outbox grew by %d, want 1", after-before)
	}
	entries, err := store.client.XRevRangeN(ctx, store.outboxStream, "+", "-", 1).Result()
	if err != nil {
		t.Fatalf("XREVRANGE outbox: %v", err)
	}
	entry := entries[0]
	if entry.Values["type"] != "round_settled" {
		t.Errorf("entry type = %v, want %q", entry.Values["type"], "round_settled")
	}
	if entry.Values["dust"] != "0" {
		t.Errorf("entry dust = %v, want %q", entry.Values["dust"], "0")
	}
	if entry.Values["winning_outcome"] != "0" {
		t.Errorf("entry winning_outcome = %v, want %q", entry.Values["winning_outcome"], "0")
	}

	// Conservation: settlement only moves tokens between wallets, so
	// Σ wallets + Dust must equal what the room started with. Pools is a
	// separate read-model settle_round.lua never touches (its KEYS list
	// has no pools key), so its total staying at the pre-settlement
	// figure is a distinct fact, not part of the balance invariant.
	sumWallets, err := sumHashValues(ctx, store, RoomWalletsKey(roomID))
	if err != nil {
		t.Fatalf("sum wallets: %v", err)
	}
	if sumWallets+int64(settlement.Dust) != 1500 {
		t.Errorf("wallets(%d) + dust(%d) = %d, want 1500", sumWallets, settlement.Dust, sumWallets+int64(settlement.Dust))
	}

	_, poolsTotal, err := store.Pools(ctx, roundID)
	if err != nil {
		t.Fatalf("Pools(): %v", err)
	}
	if poolsTotal != 500 {
		t.Errorf("pools total = %d, want 500 (unchanged by settlement)", poolsTotal)
	}
}

func sumHashValues(ctx context.Context, store *Store, key string) (int64, error) {
	fields, err := store.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	var sum int64
	for _, v := range fields {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, err
		}
		sum += n
	}
	return sum, nil
}
