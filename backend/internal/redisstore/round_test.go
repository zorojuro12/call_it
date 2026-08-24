package redisstore

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func TestCreateRound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roundID := testID(t, "round")
	roomID := testID(t, "room")
	lockAt := time.Now().Add(30 * time.Second)

	if err := store.CreateRound(ctx, roundID, roomID, 3, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}

	fields, err := store.client.HGetAll(ctx, RoundKey(roundID)).Result()
	if err != nil {
		t.Fatalf("HGETALL round:%s: %v", roundID, err)
	}
	if fields["room_id"] != roomID {
		t.Errorf("room_id = %q, want %q", fields["room_id"], roomID)
	}
	if fields["status"] != "open" {
		t.Errorf("status = %q, want %q", fields["status"], "open")
	}
	if fields["outcome_count"] != "3" {
		t.Errorf("outcome_count = %q, want %q", fields["outcome_count"], "3")
	}
	if fields["lock_at_ms"] != strconv.FormatInt(lockAt.UnixMilli(), 10) {
		t.Errorf("lock_at_ms = %q, want %q", fields["lock_at_ms"], strconv.FormatInt(lockAt.UnixMilli(), 10))
	}

	poolFields, err := store.client.HGetAll(ctx, RoundPoolsKey(roundID)).Result()
	if err != nil {
		t.Fatalf("HGETALL round:%s:pools: %v", roundID, err)
	}
	wantPools := map[string]string{"0": "0", "1": "0", "2": "0", "total": "0"}
	if len(poolFields) != len(wantPools) {
		t.Errorf("pools has %d fields, want %d: %v", len(poolFields), len(wantPools), poolFields)
	}
	for k, v := range wantPools {
		if poolFields[k] != v {
			t.Errorf("pools[%q] = %q, want %q", k, poolFields[k], v)
		}
	}

	round, err := store.Round(ctx, roundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if round.Status != domain.RoundOpen {
		t.Errorf("Round().Status = %q, want %q", round.Status, domain.RoundOpen)
	}
	if round.OutcomeCount != 3 {
		t.Errorf("Round().OutcomeCount = %d, want 3", round.OutcomeCount)
	}
	if round.ResolvedOutcome != -1 {
		t.Errorf("Round().ResolvedOutcome = %d, want -1", round.ResolvedOutcome)
	}

	pools, total, err := store.Pools(ctx, roundID)
	if err != nil {
		t.Fatalf("Pools() = %v, want nil", err)
	}
	wantSlice := []domain.Tokens{0, 0, 0}
	if len(pools) != len(wantSlice) {
		t.Fatalf("Pools() = %v, want %v", pools, wantSlice)
	}
	for i := range wantSlice {
		if pools[i] != wantSlice[i] {
			t.Errorf("Pools()[%d] = %d, want %d", i, pools[i], wantSlice[i])
		}
	}
	if total != 0 {
		t.Errorf("Pools() total = %d, want 0", total)
	}

	for _, n := range []int{1, 5} {
		badRoundID := testID(t, "round")
		err := store.CreateRound(ctx, badRoundID, roomID, n, lockAt)
		if !errors.Is(err, domain.ErrInvalidOutcomeCount) {
			t.Errorf("CreateRound() with outcomeCount %d error = %v, want ErrInvalidOutcomeCount", n, err)
		}
		exists, err := store.client.Exists(ctx, RoundKey(badRoundID)).Result()
		if err != nil {
			t.Fatalf("EXISTS round:%s: %v", badRoundID, err)
		}
		if exists != 0 {
			t.Errorf("EXISTS round:%s = %d, want 0 — invalid outcome count must write nothing", badRoundID, exists)
		}
	}

	if _, err := store.Round(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Round(\"missing\") error = %v, want ErrNotFound", err)
	}
}

func TestRound_MalformedFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("malformed outcome_count", func(t *testing.T) {
		roundID := testID(t, "round")
		if err := store.client.HSet(ctx, RoundKey(roundID), "room_id", "r1", "status", "open", "outcome_count", "not-a-number", "lock_at_ms", "1000").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, err := store.Round(ctx, roundID); err == nil {
			t.Errorf("Round() with malformed outcome_count = nil error, want an error")
		}
	})

	t.Run("malformed lock_at_ms", func(t *testing.T) {
		roundID := testID(t, "round")
		if err := store.client.HSet(ctx, RoundKey(roundID), "room_id", "r1", "status", "open", "outcome_count", "3", "lock_at_ms", "not-a-number").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, err := store.Round(ctx, roundID); err == nil {
			t.Errorf("Round() with malformed lock_at_ms = nil error, want an error")
		}
	})

	t.Run("malformed resolved_outcome", func(t *testing.T) {
		roundID := testID(t, "round")
		if err := store.client.HSet(ctx, RoundKey(roundID), "room_id", "r1", "status", "resolved", "outcome_count", "3", "lock_at_ms", "1000", "resolved_outcome", "not-a-number").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, err := store.Round(ctx, roundID); err == nil {
			t.Errorf("Round() with malformed resolved_outcome = nil error, want an error")
		}
	})
}

func TestPools_Malformed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("malformed total", func(t *testing.T) {
		roundID := testID(t, "round")
		if err := store.client.HSet(ctx, RoundPoolsKey(roundID), "0", "0", "total", "not-a-number").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, _, err := store.Pools(ctx, roundID); err == nil {
			t.Errorf("Pools() with malformed total = nil error, want an error")
		}
	})

	t.Run("malformed pool", func(t *testing.T) {
		roundID := testID(t, "round")
		if err := store.client.HSet(ctx, RoundPoolsKey(roundID), "0", "not-a-number", "total", "0").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, _, err := store.Pools(ctx, roundID); err == nil {
			t.Errorf("Pools() with malformed pool = nil error, want an error")
		}
	})

	t.Run("not found", func(t *testing.T) {
		if _, _, err := store.Pools(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Pools() on missing round error = %v, want ErrNotFound", err)
		}
	})
}

func TestLockRound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roundID := testID(t, "round")
	roomID := testID(t, "room")
	lockAt := time.Now().Add(30 * time.Second)

	if err := store.CreateRound(ctx, roundID, roomID, 3, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}

	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() = %v, want nil", err)
	}

	status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
	if err != nil {
		t.Fatalf("HGET round status: %v", err)
	}
	if status != "locked" {
		t.Errorf("status = %q, want %q", status, "locked")
	}

	round, err := store.Round(ctx, roundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if round.Status != domain.RoundLocked {
		t.Errorf("Round().Status = %q, want %q", round.Status, domain.RoundLocked)
	}

	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 500); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if err := store.JoinRoom(ctx, roomID, "u1", 500); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 200, IdempotencyKey: testID(t, "idem"),
	}); !errors.Is(err, ErrPoolLocked) {
		t.Errorf("PlaceWager() against a locked round error = %v, want ErrPoolLocked", err)
	}
}

func TestLockRound_AlreadyLocked(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roundID := testID(t, "round")
	roomID := testID(t, "room")
	lockAt := time.Now().Add(30 * time.Second)

	if err := store.CreateRound(ctx, roundID, roomID, 3, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}
	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() first = %v, want nil", err)
	}

	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() second = %v, want nil (benign no-op)", err)
	}

	status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
	if err != nil {
		t.Fatalf("HGET round status: %v", err)
	}
	if status != "locked" {
		t.Errorf("status = %q, want %q", status, "locked")
	}
}

func TestLockRound_Terminal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, terminalStatus := range []string{"resolved", "refunded"} {
		t.Run(terminalStatus, func(t *testing.T) {
			roundID := testID(t, "round")
			roomID := testID(t, "room")
			lockAt := time.Now().Add(30 * time.Second)

			if err := store.CreateRound(ctx, roundID, roomID, 3, lockAt); err != nil {
				t.Fatalf("CreateRound() = %v, want nil", err)
			}
			if err := store.client.HSet(ctx, RoundKey(roundID), "status", terminalStatus).Err(); err != nil {
				t.Fatalf("HSET round status %s: %v", terminalStatus, err)
			}

			err := store.LockRound(ctx, roundID)
			if !errors.Is(err, ErrRoundTerminal) {
				t.Fatalf("LockRound() on %s round error = %v, want ErrRoundTerminal", terminalStatus, err)
			}

			status, err := store.client.HGet(ctx, RoundKey(roundID), "status").Result()
			if err != nil {
				t.Fatalf("HGET round status: %v", err)
			}
			if status != terminalStatus {
				t.Errorf("status = %q, want unchanged %q", status, terminalStatus)
			}
		})
	}
}
