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
