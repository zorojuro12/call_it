package round

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

func TestScheduleEndSessionFoldsAfterTheWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)
	svc.sessionGrace = 200 * time.Millisecond

	roomID, userID, w := endSessionFixture(t, store, svc, ctx)

	svc.ScheduleEndSession(roomID, userID, false)
	time.Sleep(500 * time.Millisecond)

	acct, err := store.User(ctx, userID)
	if err != nil {
		t.Fatalf("User() = %v, want nil", err)
	}
	want := domain.ApplySessionResult(5000, 1000, w)
	if acct.Balance != want {
		t.Errorf("User().Balance = %d, want %d", acct.Balance, want)
	}
	if _, err := store.Balance(ctx, roomID, userID); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("Balance() after scheduled end error = %v, want ErrNotFound", err)
	}
}

func TestScheduleEndSessionSkipsGuests(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)
	svc.sessionGrace = 200 * time.Millisecond

	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	guestID := testID(t, "guest")
	if _, err := store.JoinRoom(ctx, roomID, guestID, 1000); err != nil {
		t.Fatalf("JoinRoom(guest) = %v, want nil", err)
	}

	svc.ScheduleEndSession(roomID, guestID, true)
	time.Sleep(500 * time.Millisecond)

	balance, err := store.Balance(ctx, roomID, guestID)
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != 1000 {
		t.Errorf("Balance() = %d, want 1000 (guest never scheduled)", balance)
	}
}

func TestResumeSessionCancelsAPendingEnd(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)
	svc.sessionGrace = 200 * time.Millisecond

	roomID, userID, w := endSessionFixture(t, store, svc, ctx)

	svc.ScheduleEndSession(roomID, userID, false)
	time.Sleep(50 * time.Millisecond)
	svc.ResumeSession(roomID, userID)
	time.Sleep(500 * time.Millisecond)

	acct, err := store.User(ctx, userID)
	if err != nil {
		t.Fatalf("User() = %v, want nil", err)
	}
	if acct.Balance != 5000 {
		t.Errorf("User().Balance = %d, want 5000 (untouched, no fold ran)", acct.Balance)
	}
	balance, err := store.Balance(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != w {
		t.Errorf("Balance() = %d, want %d (session survived intact)", balance, w)
	}
	opening, err := store.OpeningStake(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("OpeningStake() = %v, want nil", err)
	}
	if opening != 1000 {
		t.Errorf("OpeningStake() = %d, want 1000", opening)
	}
}

func TestResumeSessionWithNothingPending(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)

	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	userID := testID(t, "user")
	if _, err := store.JoinRoom(ctx, roomID, userID, 1000); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}

	svc.ResumeSession(roomID, userID)

	balance, err := store.Balance(ctx, roomID, userID)
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != 1000 {
		t.Errorf("Balance() = %d, want 1000 (unaffected)", balance)
	}
}

func TestServiceBalance(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(context.Background(), store, bc)

	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	userID := testID(t, "user")
	if _, err := store.JoinRoom(ctx, roomID, userID, 1000); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}

	got, err := svc.Balance(roomID, userID)
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if got != 1000 {
		t.Errorf("Balance() = %d, want 1000", got)
	}

	if _, err := svc.Balance(roomID, "never-joined"); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("Balance(never-joined) error = %v, want ErrNotFound", err)
	}
}
