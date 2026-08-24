package redisstore

import (
	"context"
	"errors"
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

func TestSettleRound_Idempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	roomID, roundID := setupSettleRound(t, store, "host1", 500, 3,
		map[string]domain.Tokens{"u1": 500, "u2": 500, "u3": 500},
		[]domain.Stake{
			{UserID: "u1", Outcome: 0, Amount: 100},
			{UserID: "u2", Outcome: 1, Amount: 300},
			{UserID: "u3", Outcome: 0, Amount: 100},
		})

	if _, err := store.SettleRound(ctx, roundID, 0, testID(t, "idem")); err != nil {
		t.Fatalf("SettleRound() first = %v, want nil", err)
	}

	before := map[string]string{}
	for _, userID := range []string{"u1", "u2", "u3"} {
		v, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
		if err != nil {
			t.Fatalf("HGET wallets %s: %v", userID, err)
		}
		before[userID] = v
	}
	xlenBefore, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox before: %v", err)
	}

	_, err = store.SettleRound(ctx, roundID, 0, testID(t, "idem"))
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("SettleRound() second error = %v, want ErrAlreadySettled", err)
	}

	for _, userID := range []string{"u1", "u2", "u3"} {
		v, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
		if err != nil {
			t.Fatalf("HGET wallets %s: %v", userID, err)
		}
		if v != before[userID] {
			t.Errorf("wallet %s = %q after second settle, want unchanged %q", userID, v, before[userID])
		}
	}
	xlenAfter, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox after: %v", err)
	}
	if xlenAfter != xlenBefore {
		t.Errorf("XLEN outbox = %d after second settle, want unchanged %d", xlenAfter, xlenBefore)
	}
}

func TestSettleRound_RequiresLock(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)

	roomID := testID(t, "room")
	roundID := testID(t, "round")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 500); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if err := store.CreateRound(ctx, roundID, roomID, 3, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}
	if err := store.JoinRoom(ctx, roomID, "u1", 500); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 0, Amount: 100, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager() = %v, want nil", err)
	}
	// Round is deliberately left open — not locked.

	xlenBefore, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox before: %v", err)
	}

	_, err = store.SettleRound(ctx, roundID, 0, testID(t, "idem"))
	if !errors.Is(err, ErrNotLocked) {
		t.Fatalf("SettleRound() on open round error = %v, want ErrNotLocked", err)
	}

	balance, err := store.client.HGet(ctx, RoomWalletsKey(roomID), "u1").Result()
	if err != nil {
		t.Fatalf("HGET wallets u1: %v", err)
	}
	if balance != "400" {
		t.Errorf("wallet u1 = %q, want unchanged %q (500 - 100 stake)", balance, "400")
	}
	status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
	if err != nil {
		t.Fatalf("HGET round status: %v", err)
	}
	if status != "open" {
		t.Errorf("status = %q, want unchanged %q", status, "open")
	}
	xlenAfter, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox after: %v", err)
	}
	if xlenAfter != xlenBefore {
		t.Errorf("XLEN outbox = %d, want unchanged %d", xlenAfter, xlenBefore)
	}
}

func TestSettleRound_NobodyBackedWinner(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	roomID, roundID := setupSettleRound(t, store, "host1", 500, 3,
		map[string]domain.Tokens{"u1": 500, "u2": 500},
		[]domain.Stake{
			{UserID: "u1", Outcome: 0, Amount: 100},
			{UserID: "u2", Outcome: 1, Amount: 300},
		})

	settlement, err := store.SettleRound(ctx, roundID, 2, testID(t, "idem"))
	if err != nil {
		t.Fatalf("SettleRound() = %v, want nil", err)
	}

	if !settlement.Refunded {
		t.Errorf("Refunded = false, want true")
	}
	if settlement.Dust != 0 {
		t.Errorf("Dust = %d, want 0", settlement.Dust)
	}
	for _, r := range settlement.Results {
		if r.Net != 0 {
			t.Errorf("Results[%s].Net = %d, want 0", r.UserID, r.Net)
		}
	}

	for userID, want := range map[string]string{"u1": "500", "u2": "500"} {
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
	if status != "refunded" {
		t.Errorf("status = %q, want %q", status, "refunded")
	}

	exists, err := store.client.HExists(ctx, RoundKey(roundID), "resolved_outcome").Result()
	if err != nil {
		t.Fatalf("HEXISTS resolved_outcome: %v", err)
	}
	if exists {
		t.Errorf("HEXISTS resolved_outcome = true, want false — a refunded round never records a winning outcome")
	}

	sumWallets, err := sumHashValues(ctx, store, RoomWalletsKey(roomID))
	if err != nil {
		t.Fatalf("sum wallets: %v", err)
	}
	if sumWallets != 1000 {
		t.Errorf("Σ wallets = %d, want 1000", sumWallets)
	}
}

func TestRefundRound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	roomID, roundID := setupSettleRound(t, store, "host1", 500, 3,
		map[string]domain.Tokens{"u1": 500, "u2": 500},
		[]domain.Stake{
			{UserID: "u1", Outcome: 0, Amount: 100},
			{UserID: "u1", Outcome: 2, Amount: 50},
			{UserID: "u2", Outcome: 1, Amount: 300},
		})

	xlenBefore, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox before: %v", err)
	}

	total, err := store.RefundRound(ctx, roundID, testID(t, "idem"))
	if err != nil {
		t.Fatalf("RefundRound() = %v, want nil", err)
	}
	if total != 450 {
		t.Errorf("RefundRound() = %d, want 450", total)
	}

	for userID, want := range map[string]string{"u1": "500", "u2": "500"} {
		got, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
		if err != nil {
			t.Fatalf("HGET wallets %s: %v", userID, err)
		}
		if got != want {
			t.Errorf("HGET wallets %s = %q, want %q — both of u1's stakes on different outcomes must come back", userID, got, want)
		}
	}

	status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
	if err != nil {
		t.Fatalf("HGET round status: %v", err)
	}
	if status != "refunded" {
		t.Errorf("status = %q, want %q", status, "refunded")
	}

	exists, err := store.client.HExists(ctx, RoundKey(roundID), "resolved_outcome").Result()
	if err != nil {
		t.Fatalf("HEXISTS resolved_outcome: %v", err)
	}
	if exists {
		t.Errorf("HEXISTS resolved_outcome = true, want false")
	}

	xlenAfter, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox after: %v", err)
	}
	if xlenAfter != xlenBefore+1 {
		t.Errorf("XLEN outbox grew by %d, want 1", xlenAfter-xlenBefore)
	}
	entries, err := store.client.XRevRangeN(ctx, store.outboxStream, "+", "-", 1).Result()
	if err != nil {
		t.Fatalf("XREVRANGE outbox: %v", err)
	}
	if entries[0].Values["type"] != "round_refunded" {
		t.Errorf("entry type = %v, want %q", entries[0].Values["type"], "round_refunded")
	}

	sumWallets, err := sumHashValues(ctx, store, RoomWalletsKey(roomID))
	if err != nil {
		t.Fatalf("sum wallets: %v", err)
	}
	if sumWallets != 1000 {
		t.Errorf("Σ wallets = %d, want 1000", sumWallets)
	}
}

func TestRefundRound_Idempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("refunding twice credits once", func(t *testing.T) {
		roomID, roundID := setupSettleRound(t, store, "host1", 500, 3,
			map[string]domain.Tokens{"u1": 500, "u2": 500},
			[]domain.Stake{
				{UserID: "u1", Outcome: 0, Amount: 100},
				{UserID: "u2", Outcome: 1, Amount: 300},
			})

		if _, err := store.RefundRound(ctx, roundID, testID(t, "idem")); err != nil {
			t.Fatalf("RefundRound() first = %v, want nil", err)
		}

		before := map[string]string{}
		for _, userID := range []string{"u1", "u2"} {
			v, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
			if err != nil {
				t.Fatalf("HGET wallets %s: %v", userID, err)
			}
			before[userID] = v
		}
		xlenBefore, err := store.client.XLen(ctx, store.outboxStream).Result()
		if err != nil {
			t.Fatalf("XLEN outbox before: %v", err)
		}

		_, err = store.RefundRound(ctx, roundID, testID(t, "idem"))
		if !errors.Is(err, ErrAlreadySettled) {
			t.Fatalf("RefundRound() second error = %v, want ErrAlreadySettled", err)
		}

		for _, userID := range []string{"u1", "u2"} {
			v, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
			if err != nil {
				t.Fatalf("HGET wallets %s: %v", userID, err)
			}
			if v != before[userID] {
				t.Errorf("wallet %s = %q after second refund, want unchanged %q", userID, v, before[userID])
			}
		}
		xlenAfter, err := store.client.XLen(ctx, store.outboxStream).Result()
		if err != nil {
			t.Fatalf("XLEN outbox after: %v", err)
		}
		if xlenAfter != xlenBefore {
			t.Errorf("XLEN outbox = %d after second refund, want unchanged %d", xlenAfter, xlenBefore)
		}
	})

	t.Run("a resolved round rejects a refund", func(t *testing.T) {
		_, roundID := setupSettleRound(t, store, "host1", 500, 3,
			map[string]domain.Tokens{"u1": 500},
			[]domain.Stake{{UserID: "u1", Outcome: 0, Amount: 100}})

		if _, err := store.SettleRound(ctx, roundID, 0, testID(t, "idem")); err != nil {
			t.Fatalf("SettleRound() = %v, want nil", err)
		}

		_, err := store.RefundRound(ctx, roundID, testID(t, "idem"))
		if !errors.Is(err, ErrAlreadySettled) {
			t.Fatalf("RefundRound() on resolved round error = %v, want ErrAlreadySettled", err)
		}
	})
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
