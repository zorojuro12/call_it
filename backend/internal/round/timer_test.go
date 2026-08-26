package round

import (
	"context"
	"encoding/json"
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
