package wager

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// stubBroadcaster records every Broadcast call, mirroring
// internal/round's own test double so both packages assert the same
// way against the shared Broadcaster interface.
type stubBroadcaster struct {
	calls []broadcastCall
}

type broadcastCall struct {
	roomID  string
	payload []byte
}

func (b *stubBroadcaster) Broadcast(roomID string, payload []byte) {
	b.calls = append(b.calls, broadcastCall{roomID: roomID, payload: payload})
}

// setupOpenRound creates a room with an open round of outcomeCount
// outcomes, joins each of players at the given balance, and returns the
// room and round IDs.
func setupOpenRound(t *testing.T, store *redisstore.Store, hostID string, buyIn domain.Tokens, outcomeCount int, players map[string]domain.Tokens) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()
	roomID = testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), hostID, buyIn); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if _, err := store.JoinRoom(ctx, roomID, hostID, buyIn); err != nil {
		t.Fatalf("JoinRoom(host) = %v, want nil", err)
	}
	for userID, balance := range players {
		if _, err := store.JoinRoom(ctx, roomID, userID, balance); err != nil {
			t.Fatalf("JoinRoom(%s) = %v, want nil", userID, err)
		}
	}
	roundID = testID(t, "round")
	outcomes := make([]string, outcomeCount)
	for i := range outcomes {
		outcomes[i] = testID(t, "outcome")
	}
	if err := store.CreateRound(ctx, roundID, roomID, "Q?", outcomes, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("CreateRound() = %v, want nil", err)
	}
	return roomID, roundID
}

func TestPlace(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID, _ := setupOpenRound(t, store, "host1", 1000, 2, map[string]domain.Tokens{"u1": 1000})

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	got, err := svc.Place(ctx, Request{
		RoomID:         roomID,
		UserID:         "u1",
		Outcome:        0,
		Amount:         200,
		IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("Place() = %v, want nil", err)
	}

	if got.Balance != 800 {
		t.Errorf("Place().Balance = %d, want 800", got.Balance)
	}
	wantPools := []domain.Tokens{200, 0}
	if len(got.Pools) != len(wantPools) || got.Pools[0] != wantPools[0] || got.Pools[1] != wantPools[1] {
		t.Errorf("Place().Pools = %v, want %v", got.Pools, wantPools)
	}
	if got.Total != 200 {
		t.Errorf("Place().Total = %d, want 200", got.Total)
	}
	if got.Bettors != 1 {
		t.Errorf("Place().Bettors = %d, want 1", got.Bettors)
	}
	// The host cannot wager, so Players excludes them — assert 1, not 2.
	if got.Players != 1 {
		t.Errorf("Place().Players = %d, want 1 (host excluded)", got.Players)
	}
	wantMult := domain.Multipliers(200, wantPools)
	for i := range wantMult {
		if got.Multipliers[i] != wantMult[i] {
			t.Errorf("Place().Multipliers[%d] = %v, want %v", i, got.Multipliers[i], wantMult[i])
		}
	}
}
