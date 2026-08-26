package round

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

func TestEndSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)

	t.Run("a win carries the net gain to the account", func(t *testing.T) {
		u1 := testID(t, "user")
		u3 := testID(t, "user")
		if err := store.CreateUser(ctx, redisstore.User{
			ID: u1, Email: u1 + "@example.com", DisplayName: "Ada",
			PasswordHash: "hash", Balance: 5000,
		}); err != nil {
			t.Fatalf("CreateUser() = %v, want nil", err)
		}

		roomID := testID(t, "room")
		if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
			t.Fatalf("CreateRoom() = %v, want nil", err)
		}
		if _, err := store.JoinRoom(ctx, roomID, u1, 1000); err != nil {
			t.Fatalf("JoinRoom(u1) = %v, want nil", err)
		}
		if _, err := store.JoinRoom(ctx, roomID, u3, 1000); err != nil {
			t.Fatalf("JoinRoom(u3) = %v, want nil", err)
		}

		roundID := testID(t, "round")
		if err := store.CreateRound(ctx, roundID, roomID, "Q?", []string{"Yes", "No"}, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("CreateRound() = %v, want nil", err)
		}
		if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: u1,
			Outcome: 0, Amount: 100, IdempotencyKey: testID(t, "idem"),
		}); err != nil {
			t.Fatalf("PlaceWager(u1) = %v, want nil", err)
		}
		if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: u3,
			Outcome: 1, Amount: 600, IdempotencyKey: testID(t, "idem"),
		}); err != nil {
			t.Fatalf("PlaceWager(u3) = %v, want nil", err)
		}
		if err := store.LockRound(ctx, roundID); err != nil {
			t.Fatalf("LockRound() = %v, want nil", err)
		}
		if _, err := svc.Resolve(ctx, roomID, "host1", 0); err != nil {
			t.Fatalf("Resolve() = %v, want nil", err)
		}

		// u1 is the sole backer of the winning outcome: payout =
		// stake * (total pool / winning pool) = 100 * (700/100) = 700.
		// Wallet: 1000 - 100 + 700 = 1600. Session net: +600.
		balance, err := store.Balance(ctx, roomID, u1)
		if err != nil {
			t.Fatalf("Balance() = %v, want nil", err)
		}
		if balance != 1600 {
			t.Fatalf("Balance() = %d, want 1600 (test setup assumption)", balance)
		}

		newBalance, err := svc.EndSession(ctx, roomID, u1, false)
		if err != nil {
			t.Fatalf("EndSession() = %v, want nil", err)
		}
		if newBalance != 5600 {
			t.Errorf("EndSession() = %d, want 5600 (5000 persistent + 600 net)", newBalance)
		}
		acct, err := store.User(ctx, u1)
		if err != nil {
			t.Fatalf("User() = %v, want nil", err)
		}
		if acct.Balance != 5600 {
			t.Errorf("User().Balance = %d, want 5600", acct.Balance)
		}
	})

	t.Run("a loss carries the net loss to the account", func(t *testing.T) {
		u1 := testID(t, "user")
		if err := store.CreateUser(ctx, redisstore.User{
			ID: u1, Email: u1 + "@example.com", DisplayName: "Ada",
			PasswordHash: "hash", Balance: 5000,
		}); err != nil {
			t.Fatalf("CreateUser() = %v, want nil", err)
		}

		roomID := testID(t, "room")
		if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
			t.Fatalf("CreateRoom() = %v, want nil", err)
		}
		if _, err := store.JoinRoom(ctx, roomID, u1, 1000); err != nil {
			t.Fatalf("JoinRoom(u1) = %v, want nil", err)
		}

		roundID := testID(t, "round")
		if err := store.CreateRound(ctx, roundID, roomID, "Q?", []string{"Yes", "No"}, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("CreateRound() = %v, want nil", err)
		}
		if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
			RoomID: roomID, RoundID: roundID, UserID: u1,
			Outcome: 0, Amount: 600, IdempotencyKey: testID(t, "idem"),
		}); err != nil {
			t.Fatalf("PlaceWager(u1) = %v, want nil", err)
		}

		balance, err := store.Balance(ctx, roomID, u1)
		if err != nil {
			t.Fatalf("Balance() = %v, want nil", err)
		}
		if balance != 400 {
			t.Fatalf("Balance() = %d, want 400 (test setup assumption)", balance)
		}

		newBalance, err := svc.EndSession(ctx, roomID, u1, false)
		if err != nil {
			t.Fatalf("EndSession() = %v, want nil", err)
		}
		if newBalance != 4400 {
			t.Errorf("EndSession() = %d, want 4400 (5000 persistent - 600 net loss)", newBalance)
		}
	})
}

func TestEndSessionGuest(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)

	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	guest1 := testID(t, "guest")
	if _, err := store.JoinRoom(ctx, roomID, guest1, 1000); err != nil {
		t.Fatalf("JoinRoom(guest1) = %v, want nil", err)
	}

	newBalance, err := svc.EndSession(ctx, roomID, guest1, true)
	if err != nil {
		t.Fatalf("EndSession(guest) = %v, want nil", err)
	}
	if newBalance != 0 {
		t.Errorf("EndSession(guest) = %d, want 0", newBalance)
	}
	if _, err := store.User(ctx, guest1); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("User(guest1) error = %v, want ErrNotFound (a guest has no persistent identity)", err)
	}

	neverJoined := testID(t, "user")
	if _, err := svc.EndSession(ctx, roomID, neverJoined, false); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("EndSession(never-joined) error = %v, want ErrNotFound", err)
	}
}
