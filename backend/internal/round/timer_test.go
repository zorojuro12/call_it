package round

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// pollUntil polls fn every 20ms until it reports true or timeout elapses,
// failing the test if timeout is reached first.
func pollUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !fn() {
		t.Fatalf("condition not met within %v", timeout)
	}
}

// openAndWatch creates a room and a round directly through the store
// (bypassing Service.Open's MinLockIn/MaxLockIn bound, which a 100ms
// test lock window would fail) and starts the timer exactly as Open
// does, so these timer tests can run fast without weakening Open's own
// validation, proven separately in TestOpenInvalid.
func openAndWatch(t *testing.T, store *redisstore.Store, svc *Service, hostID string, buyIn domain.Tokens, lockIn time.Duration) (roomID, roundID string, lockAt time.Time) {
	t.Helper()
	ctx := context.Background()
	roomID = testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), hostID, buyIn); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	roundID = testID(t, "round")
	lockAt = time.Now().Add(lockIn)
	if err := store.CreateRound(ctx, roundID, roomID, "Q?", []string{"Yes", "No"}, lockAt); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}
	go svc.watch(context.Background(), roomID, roundID, lockAt)
	return roomID, roundID, lockAt
}

func TestTimerLocks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	_, roundID, _ := openAndWatch(t, store, svc, "host1", 1000, 100*time.Millisecond)

	pollUntil(t, 2*time.Second, func() bool {
		rd, err := store.Round(ctx, roundID)
		return err == nil && rd.Status == domain.RoundLocked
	})

	rd, err := store.Round(ctx, roundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if rd.Status != domain.RoundLocked {
		t.Fatalf("Round().Status = %q, want %q", rd.Status, domain.RoundLocked)
	}

	var found bool
	for _, c := range bc.Calls() {
		env := decodeEnvelope(t, c.payload)
		if env.Type != "round_locked" {
			continue
		}
		var data LockedEvent
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatalf("decode round_locked data: %v", err)
		}
		if data.RoundID == roundID {
			found = true
		}
	}
	if !found {
		t.Errorf("no round_locked broadcast found for round %s in %+v", roundID, bc.Calls())
	}
}

// TestLockedRejectsWagers proves the timer (CP1) and place_wager.lua's
// own TIME check agree in practice — the lockout guarantee is spec §4's
// client-latency defence and is worth one integration test rather than
// trusting two mechanisms to line up. This test's meaningful pass
// depends on CP1's implementation existing: if it failed, the defect
// would be in the timer or the round's lock_at_ms, not here.
func TestLockedRejectsWagers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	roomID, roundID, _ := openAndWatch(t, store, svc, "host1", 1000, 100*time.Millisecond)
	if _, err := store.JoinRoom(ctx, roomID, "u1", 1000); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}

	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 0, Amount: 50, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager() before lock = %v, want nil", err)
	}

	pollUntil(t, 2*time.Second, func() bool {
		rd, err := store.Round(ctx, roundID)
		return err == nil && rd.Status == domain.RoundLocked
	})

	balanceBefore, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}

	_, err = store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 50, IdempotencyKey: testID(t, "idem"),
	})
	if !errors.Is(err, redisstore.ErrPoolLocked) {
		t.Fatalf("PlaceWager() after lock error = %v, want ErrPoolLocked", err)
	}

	balanceAfter, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balanceAfter != balanceBefore {
		t.Errorf("Balance() after rejected wager = %d, want unchanged %d", balanceAfter, balanceBefore)
	}
}

func TestTimerSkipsTerminal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	roomID, roundID, _ := openAndWatch(t, store, svc, "host1", 1000, 2*time.Second)
	if _, err := store.JoinRoom(ctx, roomID, "u1", 1000); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 0, Amount: 100, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager() = %v, want nil", err)
	}

	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() = %v, want nil", err)
	}
	if _, err := store.SettleRound(ctx, roundID, 0, testID(t, "idem")); err != nil {
		t.Fatalf("SettleRound() = %v, want nil", err)
	}

	time.Sleep(2500 * time.Millisecond)

	rd, err := store.Round(ctx, roundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if rd.Status != domain.RoundResolved {
		t.Errorf("Round().Status = %q, want %q (unchanged by the timer)", rd.Status, domain.RoundResolved)
	}

	for _, c := range bc.Calls() {
		env := decodeEnvelope(t, c.payload)
		if env.Type == "round_locked" {
			t.Errorf("unexpected round_locked broadcast for an already-resolved round: %+v", c)
		}
	}
}

func TestAutoRefund(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	bc := &stubBroadcaster{}
	svc := NewService(store, bc)
	svc.refundGrace = 300 * time.Millisecond

	roomID, roundID, _ := openAndWatch(t, store, svc, "host1", 1000, 100*time.Millisecond)
	if _, err := store.JoinRoom(ctx, roomID, "u1", 1000); err != nil {
		t.Fatalf("JoinRoom(u1) = %v, want nil", err)
	}
	if _, err := store.JoinRoom(ctx, roomID, "u2", 1000); err != nil {
		t.Fatalf("JoinRoom(u2) = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 0, Amount: 100, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager(u1) = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u2",
		Outcome: 1, Amount: 150, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager(u2) = %v, want nil", err)
	}

	pollUntil(t, 3*time.Second, func() bool {
		rd, err := store.Round(ctx, roundID)
		return err == nil && rd.Status == domain.RoundRefunded
	})

	rd, err := store.Round(ctx, roundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if rd.Status != domain.RoundRefunded {
		t.Fatalf("Round().Status = %q, want %q", rd.Status, domain.RoundRefunded)
	}

	u1Balance, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance(u1) = %v, want nil", err)
	}
	if u1Balance != 1000 {
		t.Errorf("Balance(u1) = %d, want 1000 (pre-wager)", u1Balance)
	}
	u2Balance, err := store.Balance(ctx, roomID, "u2")
	if err != nil {
		t.Fatalf("Balance(u2) = %v, want nil", err)
	}
	if u2Balance != 1000 {
		t.Errorf("Balance(u2) = %d, want 1000 (pre-wager)", u2Balance)
	}

	if _, err := store.CurrentRound(ctx, roomID); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("CurrentRound() after auto-refund error = %v, want ErrNotFound", err)
	}

	var found bool
	for _, c := range bc.Calls() {
		env := decodeEnvelope(t, c.payload)
		if env.Type != "round_refunded" {
			continue
		}
		var data RefundedEvent
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatalf("decode round_refunded data: %v", err)
		}
		if data.RoundID == roundID && data.Total == 250 {
			found = true
		}
	}
	if !found {
		t.Errorf("no round_refunded broadcast with RoundID %s and Total 250 found in %+v", roundID, bc.Calls())
	}
}
