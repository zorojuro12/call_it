package redisstore

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func TestCreateRoom(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")

	if err := store.CreateRoom(ctx, roomID, "WXYZ", "host1", 500); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}

	fields, err := store.client.HGetAll(ctx, RoomKey(roomID)).Result()
	if err != nil {
		t.Fatalf("HGETALL room:%s: %v", roomID, err)
	}
	if fields["host_id"] != "host1" {
		t.Errorf("host_id = %q, want %q", fields["host_id"], "host1")
	}
	if fields["buy_in"] != "500" {
		t.Errorf("buy_in = %q, want %q", fields["buy_in"], "500")
	}
	if fields["status"] != "open" {
		t.Errorf("status = %q, want %q", fields["status"], "open")
	}
	createdAtMs, err := strconv.ParseInt(fields["created_at"], 10, 64)
	if err != nil {
		t.Fatalf("created_at %q is not a parsable Unix millisecond timestamp: %v", fields["created_at"], err)
	}
	if createdAtMs <= 0 {
		t.Errorf("created_at = %d, want a positive Unix millisecond timestamp", createdAtMs)
	}

	code, err := store.client.Get(ctx, RoomCodeKey("WXYZ")).Result()
	if err != nil {
		t.Fatalf("GET code:WXYZ: %v", err)
	}
	if code != roomID {
		t.Errorf("code:WXYZ = %q, want %q", code, roomID)
	}

	gotID, err := store.RoomByCode(ctx, "WXYZ")
	if err != nil {
		t.Fatalf("RoomByCode() = %v, want nil", err)
	}
	if gotID != roomID {
		t.Errorf("RoomByCode() = %q, want %q", gotID, roomID)
	}

	room, err := store.Room(ctx, roomID)
	if err != nil {
		t.Fatalf("Room() = %v, want nil", err)
	}
	if room.HostID != "host1" || room.BuyIn != domain.Tokens(500) || room.Status != "open" {
		t.Errorf("Room() = %+v, want HostID=host1 BuyIn=500 Status=open", room)
	}

	if _, err := store.RoomByCode(ctx, "NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RoomByCode(\"NOPE\") error = %v, want ErrNotFound", err)
	}

	badRoomID := testID(t, "room")
	err = store.CreateRoom(ctx, badRoomID, "BADX", "host1", 50)
	if !errors.Is(err, domain.ErrInvalidBuyIn) {
		t.Fatalf("CreateRoom() with buyIn 50 error = %v, want ErrInvalidBuyIn", err)
	}
	exists, err := store.client.Exists(ctx, RoomKey(badRoomID)).Result()
	if err != nil {
		t.Fatalf("EXISTS room:%s: %v", badRoomID, err)
	}
	if exists != 0 {
		t.Errorf("EXISTS room:%s = %d, want 0 — invalid buy-in must write nothing", badRoomID, exists)
	}
}

func TestCreateRoom_CodeCollision(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	code := testID(t, "code")
	roomA := testID(t, "room")
	roomB := testID(t, "room")

	if err := store.CreateRoom(ctx, roomA, code, "hostA", 500); err != nil {
		t.Fatalf("CreateRoom(A) = %v, want nil", err)
	}

	err := store.CreateRoom(ctx, roomB, code, "hostB", 900)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("CreateRoom(B) err = %v, want ErrAlreadyExists", err)
	}

	gotID, err := store.RoomByCode(ctx, code)
	if err != nil {
		t.Fatalf("RoomByCode() = %v, want nil", err)
	}
	if gotID != roomA {
		t.Errorf("RoomByCode() = %q, want %q (room A)", gotID, roomA)
	}

	roomAFields, err := store.Room(ctx, roomA)
	if err != nil {
		t.Fatalf("Room(A) = %v, want nil", err)
	}
	if roomAFields.HostID != "hostA" || roomAFields.BuyIn != domain.Tokens(500) {
		t.Errorf("Room(A) = %+v, want HostID=hostA BuyIn=500 (untouched)", roomAFields)
	}

	if _, err := store.Room(ctx, roomB); !errors.Is(err, ErrNotFound) {
		t.Errorf("Room(B) err = %v, want ErrNotFound — B must never have been written", err)
	}
}

func TestRoom_MalformedFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("malformed buy_in", func(t *testing.T) {
		roomID := testID(t, "room")
		if err := store.client.HSet(ctx, RoomKey(roomID), "host_id", "host1", "buy_in", "not-a-number", "status", "open", "created_at", "1000").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, err := store.Room(ctx, roomID); err == nil {
			t.Errorf("Room() with malformed buy_in = nil error, want an error")
		}
	})

	t.Run("malformed created_at", func(t *testing.T) {
		roomID := testID(t, "room")
		if err := store.client.HSet(ctx, RoomKey(roomID), "host_id", "host1", "buy_in", "500", "status", "open", "created_at", "not-a-number").Err(); err != nil {
			t.Fatalf("HSET: %v", err)
		}
		if _, err := store.Room(ctx, roomID); err == nil {
			t.Errorf("Room() with malformed created_at = nil error, want an error")
		}
	})
}

func TestBalance_Malformed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")

	if err := store.client.HSet(ctx, RoomWalletsKey(roomID), "u1", "not-a-number").Err(); err != nil {
		t.Fatalf("HSET: %v", err)
	}
	if _, err := store.Balance(ctx, roomID, "u1"); err == nil {
		t.Errorf("Balance() with malformed value = nil error, want an error")
	}
}

func TestJoinRoom(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")

	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 500); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}

	eff, err := store.JoinRoom(ctx, roomID, "u1", 500)
	if err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}
	if eff != 500 {
		t.Errorf("JoinRoom() effective = %d, want 500", eff)
	}
	balField, err := store.client.HGet(ctx, RoomWalletsKey(roomID), "u1").Result()
	if err != nil {
		t.Fatalf("HGET room:%s:wallets u1: %v", roomID, err)
	}
	if balField != "500" {
		t.Errorf("HGET wallets u1 = %q, want %q", balField, "500")
	}
	balance, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != domain.Tokens(500) {
		t.Errorf("Balance() = %d, want 500", balance)
	}

	if _, err := store.Balance(ctx, roomID, "never-joined"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Balance() for user who never joined error = %v, want ErrNotFound", err)
	}

	_, err = store.JoinRoom(ctx, roomID, "u-zero", 0)
	if !errors.Is(err, domain.ErrInvalidStake) {
		t.Fatalf("JoinRoom() with balance 0 error = %v, want ErrInvalidStake", err)
	}
	exists, err := store.client.HExists(ctx, RoomWalletsKey(roomID), "u-zero").Result()
	if err != nil {
		t.Fatalf("HEXISTS wallets u-zero: %v", err)
	}
	if exists {
		t.Errorf("HEXISTS wallets u-zero = true, want false — invalid balance must write nothing")
	}

	if _, err := store.JoinRoom(ctx, roomID, "u2", 500); err != nil {
		t.Fatalf("JoinRoom(u2) = %v, want nil", err)
	}
	// The host also holds a wallet field for room bookkeeping, but must
	// not count toward the player denominator (spec §4).
	if _, err := store.JoinRoom(ctx, roomID, "host1", 500); err != nil {
		t.Fatalf("JoinRoom(host1) = %v, want nil", err)
	}
	count, err := store.PlayerCount(ctx, roomID)
	if err != nil {
		t.Fatalf("PlayerCount() = %v, want nil", err)
	}
	if count != 2 {
		t.Errorf("PlayerCount() = %d, want 2 (host excluded)", count)
	}
}

// TestJoinRoom_Rejoin proves Amendment B4's second half: rejoining an
// existing wallet preserves whatever balance it currently holds instead
// of resetting it to the buy-in. The old unconditional HSET let any
// losing participant refresh the page and have their wallet topped back
// to the full buy-in — unlimited tokens, no tooling required.
func TestJoinRoom_Rejoin(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")

	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 500); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}

	eff1, err := store.JoinRoom(ctx, roomID, "u1", 500)
	if err != nil || eff1 != 500 {
		t.Fatalf("JoinRoom(u1) first join = (%d, %v), want (500, nil)", eff1, err)
	}

	// Simulate play driving the balance down.
	if err := store.client.HSet(ctx, RoomWalletsKey(roomID), "u1", "120").Err(); err != nil {
		t.Fatalf("HSET wallets u1 120: %v", err)
	}

	eff2, err := store.JoinRoom(ctx, roomID, "u1", 500)
	if err != nil || eff2 != 120 {
		t.Fatalf("JoinRoom(u1) rejoin = (%d, %v), want (120, nil) — the surviving balance, not a reset", eff2, err)
	}
	balance, err := store.Balance(ctx, roomID, "u1")
	if err != nil || balance != 120 {
		t.Fatalf("Balance(u1) after rejoin = (%d, %v), want (120, nil) — must not be reset to 500", balance, err)
	}

	effNew, err := store.JoinRoom(ctx, roomID, "u2", 500)
	if err != nil || effNew != 500 {
		t.Fatalf("JoinRoom(u2) genuinely new joiner = (%d, %v), want (500, nil)", effNew, err)
	}

	if _, err := store.JoinRoom(ctx, roomID, "u3", 0); !errors.Is(err, domain.ErrInvalidStake) {
		t.Fatalf("JoinRoom(u3, 0) err = %v, want ErrInvalidStake — unchanged from Phase 2", err)
	}
}
