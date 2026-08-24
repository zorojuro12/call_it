package redisstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// wagerSnapshot captures the mutable state a rejected wager must never
// touch: the wagerer's balance, the pools hash, and the outbox length.
type wagerSnapshot struct {
	balance string
	pools   map[string]string
	xlen    int64
}

func snapshotWager(t *testing.T, store *Store, roomID, roundID, userID string) wagerSnapshot {
	t.Helper()
	ctx := context.Background()

	balance, err := store.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		t.Fatalf("snapshot HGET wallets %s: %v", userID, err)
	}
	pools, err := store.client.HGetAll(ctx, RoundPoolsKey(roundID)).Result()
	if err != nil {
		t.Fatalf("snapshot HGETALL pools: %v", err)
	}
	xlen, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("snapshot XLEN outbox: %v", err)
	}

	return wagerSnapshot{balance: balance, pools: pools, xlen: xlen}
}

func assertNoMutation(t *testing.T, store *Store, roomID, roundID, userID string, before wagerSnapshot) {
	t.Helper()
	after := snapshotWager(t, store, roomID, roundID, userID)

	if after.balance != before.balance {
		t.Errorf("wallet mutated: before %q, after %q", before.balance, after.balance)
	}
	if !reflect.DeepEqual(after.pools, before.pools) {
		t.Errorf("pools mutated: before %v, after %v", before.pools, after.pools)
	}
	if after.xlen != before.xlen {
		t.Errorf("outbox mutated: before XLEN %d, after %d", before.xlen, after.xlen)
	}
}

// setupWagerRoom creates a room, a round, and joins the given players
// (userID -> balance), returning the room and round IDs.
func setupWagerRoom(t *testing.T, store *Store, hostID string, buyIn domain.Tokens, outcomeCount int, lockAt time.Time, players map[string]domain.Tokens) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()

	roomID = testID(t, "room")
	roundID = testID(t, "round")

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

	return roomID, roundID
}

func TestPlaceWager_Accept(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 500})

	result, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 200, IdempotencyKey: testID(t, "idem"),
	})
	if err != nil {
		t.Fatalf("PlaceWager() = %v, want nil", err)
	}
	if result.Balance != 300 {
		t.Errorf("Balance = %d, want 300", result.Balance)
	}
	wantPools := []domain.Tokens{0, 200, 0}
	if len(result.Pools) != len(wantPools) {
		t.Fatalf("Pools = %v, want %v", result.Pools, wantPools)
	}
	for i := range wantPools {
		if result.Pools[i] != wantPools[i] {
			t.Errorf("Pools[%d] = %d, want %d", i, result.Pools[i], wantPools[i])
		}
	}
	if result.Total != 200 {
		t.Errorf("Total = %d, want 200", result.Total)
	}
	if result.BettorCount != 1 {
		t.Errorf("BettorCount = %d, want 1", result.BettorCount)
	}

	balField, err := store.client.HGet(ctx, RoomWalletsKey(roomID), "u1").Result()
	if err != nil {
		t.Fatalf("HGET wallets u1: %v", err)
	}
	if balField != "300" {
		t.Errorf("HGET wallets u1 = %q, want %q", balField, "300")
	}

	poolFields, err := store.client.HGetAll(ctx, RoundPoolsKey(roundID)).Result()
	if err != nil {
		t.Fatalf("HGETALL pools: %v", err)
	}
	wantPoolFields := map[string]string{"0": "0", "1": "200", "2": "0", "total": "200"}
	for k, v := range wantPoolFields {
		if poolFields[k] != v {
			t.Errorf("pools[%q] = %q, want %q", k, poolFields[k], v)
		}
	}

	wagerField, err := store.client.HGet(ctx, RoundWagersKey(roundID), WagerField("u1", 1)).Result()
	if err != nil {
		t.Fatalf("HGET wagers u1:1: %v", err)
	}
	if wagerField != "200" {
		t.Errorf("HGET wagers u1:1 = %q, want %q", wagerField, "200")
	}

	bettorCount, err := store.client.SCard(ctx, RoundBettorsKey(roundID)).Result()
	if err != nil {
		t.Fatalf("SCARD bettors: %v", err)
	}
	if bettorCount != 1 {
		t.Errorf("SCARD bettors = %d, want 1", bettorCount)
	}

	entries, err := store.client.XRange(ctx, store.outboxStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE outbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("XLEN outbox = %d, want 1", len(entries))
	}
	entry := entries[0]
	wantEntryFields := map[string]string{
		"type": "wager_placed", "user": "u1", "outcome": "1", "amount": "200", "balance": "300",
	}
	for k, v := range wantEntryFields {
		got, ok := entry.Values[k]
		if !ok || got != v {
			t.Errorf("outbox entry[%q] = %v, want %q", k, got, v)
		}
	}

	// A second, different wager by the same player accumulates rather
	// than replaces.
	result2, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 100, IdempotencyKey: testID(t, "idem"),
	})
	if err != nil {
		t.Fatalf("PlaceWager() second = %v, want nil", err)
	}
	if result2.Balance != 200 {
		t.Errorf("second Balance = %d, want 200", result2.Balance)
	}
	wantPools2 := []domain.Tokens{0, 300, 0}
	for i := range wantPools2 {
		if result2.Pools[i] != wantPools2[i] {
			t.Errorf("second Pools[%d] = %d, want %d", i, result2.Pools[i], wantPools2[i])
		}
	}
	if result2.Total != 300 {
		t.Errorf("second Total = %d, want 300", result2.Total)
	}
	if result2.BettorCount != 1 {
		t.Errorf("second BettorCount = %d, want 1 — repeat wagers by the same player don't move it", result2.BettorCount)
	}
	wagerField2, err := store.client.HGet(ctx, RoundWagersKey(roundID), WagerField("u1", 1)).Result()
	if err != nil {
		t.Fatalf("HGET wagers u1:1 after second wager: %v", err)
	}
	if wagerField2 != "300" {
		t.Errorf("HGET wagers u1:1 after second = %q, want %q", wagerField2, "300")
	}
	xlen2, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox after second: %v", err)
	}
	if xlen2 != 2 {
		t.Errorf("XLEN outbox after second = %d, want 2", xlen2)
	}

	// A second player wagering moves BettorCount and Total.
	if err := store.JoinRoom(ctx, roomID, "u2", 500); err != nil {
		t.Fatalf("JoinRoom(u2) = %v, want nil", err)
	}
	result3, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u2",
		Outcome: 0, Amount: 50, IdempotencyKey: testID(t, "idem"),
	})
	if err != nil {
		t.Fatalf("PlaceWager() third = %v, want nil", err)
	}
	if result3.BettorCount != 2 {
		t.Errorf("third BettorCount = %d, want 2", result3.BettorCount)
	}
	if result3.Total != 350 {
		t.Errorf("third Total = %d, want 350", result3.Total)
	}
}

func TestPlaceWager_IdempotentReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 500})

	key := testID(t, "idem")
	req := WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 200, IdempotencyKey: key,
	}

	first, err := store.PlaceWager(ctx, req)
	if err != nil {
		t.Fatalf("PlaceWager() first = %v, want nil", err)
	}

	second, err := store.PlaceWager(ctx, req)
	if err != nil {
		t.Fatalf("PlaceWager() second (replay) = %v, want nil", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Errorf("replayed WagerResult = %+v, want deep-equal to first %+v", second, first)
	}

	balField, err := store.client.HGet(ctx, RoomWalletsKey(roomID), "u1").Result()
	if err != nil {
		t.Fatalf("HGET wallets u1: %v", err)
	}
	if balField != "300" {
		t.Errorf("HGET wallets u1 = %q, want %q — debited once", balField, "300")
	}

	poolField, err := store.client.HGet(ctx, RoundPoolsKey(roundID), "1").Result()
	if err != nil {
		t.Fatalf("HGET pools 1: %v", err)
	}
	if poolField != "200" {
		t.Errorf("HGET pools 1 = %q, want %q", poolField, "200")
	}

	bettorCount, err := store.client.SCard(ctx, RoundBettorsKey(roundID)).Result()
	if err != nil {
		t.Fatalf("SCARD bettors: %v", err)
	}
	if bettorCount != 1 {
		t.Errorf("SCARD bettors = %d, want 1", bettorCount)
	}

	xlen, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox: %v", err)
	}
	if xlen != 1 {
		t.Errorf("XLEN outbox = %d, want 1 — no duplicate outbox event", xlen)
	}

	ttl, err := store.client.TTL(ctx, IdemKey(key)).Result()
	if err != nil {
		t.Fatalf("TTL idem:%s: %v", key, err)
	}
	if ttl <= 0 || ttl > 86400*time.Second {
		t.Errorf("TTL idem:%s = %v, want (0, 86400s]", key, ttl)
	}
}

func TestPlaceWager_RejectsLockedStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 500})

	if err := store.client.HSet(ctx, RoundKey(roundID), "status", "locked").Err(); err != nil {
		t.Fatalf("HSET round status locked: %v", err)
	}

	before := snapshotWager(t, store, roomID, roundID, "u1")

	_, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 200, IdempotencyKey: testID(t, "idem"),
	})
	if !errors.Is(err, ErrPoolLocked) {
		t.Fatalf("PlaceWager() on locked round error = %v, want ErrPoolLocked", err)
	}

	assertNoMutation(t, store, roomID, roundID, "u1", before)
}

func TestPlaceWager_RejectsAfterLockInstant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("lock instant in the past rejects even though status is still open", func(t *testing.T) {
		pastLock := time.Now().Add(-1000 * time.Millisecond)
		roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, pastLock, map[string]domain.Tokens{"u1": 500})

		before := snapshotWager(t, store, roomID, roundID, "u1")

		_, err := store.PlaceWager(ctx, WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: "u1",
			Outcome: 1, Amount: 200, IdempotencyKey: testID(t, "idem"),
		})
		if !errors.Is(err, ErrPoolLocked) {
			t.Fatalf("PlaceWager() past lock_at_ms error = %v, want ErrPoolLocked", err)
		}

		assertNoMutation(t, store, roomID, roundID, "u1", before)
	})

	t.Run("lock instant in the future accepts", func(t *testing.T) {
		futureLock := time.Now().Add(30 * time.Second)
		roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, futureLock, map[string]domain.Tokens{"u1": 500})

		_, err := store.PlaceWager(ctx, WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: "u1",
			Outcome: 1, Amount: 200, IdempotencyKey: testID(t, "idem"),
		})
		if err != nil {
			t.Fatalf("PlaceWager() future lock_at_ms = %v, want nil", err)
		}
	})
}

func TestPlaceWager_RejectsInvalidOutcome(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)

	for _, outcome := range []int{3, 4, -1} {
		t.Run(fmt.Sprintf("outcome %d", outcome), func(t *testing.T) {
			roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 500})
			before := snapshotWager(t, store, roomID, roundID, "u1")

			_, err := store.PlaceWager(ctx, WagerRequest{
				RoomID: roomID, RoundID: roundID, UserID: "u1",
				Outcome: outcome, Amount: 200, IdempotencyKey: testID(t, "idem"),
			})
			if !errors.Is(err, domain.ErrInvalidOutcome) {
				t.Fatalf("PlaceWager() outcome %d error = %v, want domain.ErrInvalidOutcome", outcome, err)
			}

			assertNoMutation(t, store, roomID, roundID, "u1", before)

			exists, err := store.client.HExists(ctx, RoundPoolsKey(roundID), strconv.Itoa(outcome)).Result()
			if err != nil {
				t.Fatalf("HEXISTS pools %d: %v", outcome, err)
			}
			if exists {
				t.Errorf("HEXISTS pools %d = true, want false — no stray pool field for an invalid outcome", outcome)
			}
		})
	}
}
