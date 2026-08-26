package wager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// testEnvelope decodes a broadcast payload's {"type":...,"data":...}
// shape without depending on internal/ws (wager must not import it —
// see round.Broadcaster's doc comment for why).
type testEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, payload []byte) testEnvelope {
	t.Helper()
	var env testEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

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
// room and round IDs. hostID and every key of players must be
// collision-free across tests — the shared wager rate-limit keyspace is
// keyed by user ID alone (not by room), so a literal "u1" reused across
// test functions would let one test's quota consumption bleed into
// another's within the same 10-second window.
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
	hostID := testID(t, "host")
	u1 := testID(t, "user")
	roomID, _ := setupOpenRound(t, store, hostID, 1000, 2, map[string]domain.Tokens{u1: 1000})

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	got, err := svc.Place(ctx, Request{
		RoomID:         roomID,
		UserID:         u1,
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

func TestPlaceRejects(t *testing.T) {
	tests := []struct {
		name    string
		asUser  string // "host", "joined", or "never-joined"
		outcome int
		amount  domain.Tokens
		noRound bool
		wantErr error
	}{
		{"host wagers in own room", "host", 0, 50, false, redisstore.ErrHostCannotBet},
		{"user who never joined", "never-joined", 0, 50, false, redisstore.ErrNotInRoom},
		{"amount exceeds session wallet", "joined", 0, 5000, false, domain.ErrInsufficientFunds},
		{"outcome index out of range", "joined", 5, 50, false, domain.ErrInvalidOutcome},
		{"no open round", "joined", 0, 50, true, ErrNoActiveRound},
		{"amount is zero", "joined", 0, 0, false, domain.ErrInvalidStake},
		{"amount is negative", "joined", 0, -10, false, domain.ErrInvalidStake},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			hostID := testID(t, "host")
			joined := testID(t, "user")
			roomID, _ := setupOpenRound(t, store, hostID, 1000, 2, map[string]domain.Tokens{joined: 1000})
			if tt.noRound {
				if err := store.ClearCurrentRound(ctx, roomID); err != nil {
					t.Fatalf("ClearCurrentRound() = %v, want nil", err)
				}
			}

			userID := map[string]string{
				"host":         hostID,
				"joined":       joined,
				"never-joined": testID(t, "user"),
			}[tt.asUser]

			var balanceBefore domain.Tokens
			hadBalance := false
			if b, err := store.Balance(ctx, roomID, userID); err == nil {
				balanceBefore = b
				hadBalance = true
			}

			bc := &stubBroadcaster{}
			svc := NewService(store, bc)

			_, err := svc.Place(ctx, Request{
				RoomID:         roomID,
				UserID:         userID,
				Outcome:        tt.outcome,
				Amount:         tt.amount,
				IdempotencyKey: uuid.NewString(),
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Place() error = %v, want %v", err, tt.wantErr)
			}

			if hadBalance {
				after, err := store.Balance(ctx, roomID, userID)
				if err != nil {
					t.Fatalf("Balance() = %v, want nil", err)
				}
				if after != balanceBefore {
					t.Errorf("Balance() after rejected wager = %d, want unchanged %d", after, balanceBefore)
				}
			}
		})
	}
}

func TestPlaceIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hostID := testID(t, "host")
	u1 := testID(t, "user")
	roomID, _ := setupOpenRound(t, store, hostID, 1000, 2, map[string]domain.Tokens{u1: 1000})

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	key := uuid.NewString()
	first, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 200, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Place() first = %v, want nil", err)
	}
	if first.Balance != 800 {
		t.Fatalf("Place() first Balance = %d, want 800", first.Balance)
	}

	replay, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 200, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Place() replay = %v, want nil", err)
	}
	if replay.Balance != 800 {
		t.Errorf("Place() replay Balance = %d, want 800 (unchanged)", replay.Balance)
	}
	if replay.Total != 200 {
		t.Errorf("Place() replay Total = %d, want 200 (unchanged)", replay.Total)
	}
	if replay.Bettors != 1 {
		t.Errorf("Place() replay Bettors = %d, want 1 (unchanged)", replay.Bettors)
	}

	second, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 200, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatalf("Place() second (distinct key) = %v, want nil", err)
	}
	if second.Balance != 600 {
		t.Errorf("Place() second Balance = %d, want 600", second.Balance)
	}
	if second.Total != 400 {
		t.Errorf("Place() second Total = %d, want 400", second.Total)
	}
	if second.Bettors != 1 {
		t.Errorf("Place() second Bettors = %d, want 1 (a repeat wagerer never moves the count)", second.Bettors)
	}

	_, err = svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 200, IdempotencyKey: "abc"})
	if !errors.Is(err, ErrBadIdempotency) {
		t.Fatalf("Place() with a non-UUIDv4 key error = %v, want ErrBadIdempotency", err)
	}
}

func TestPlaceRateLimited(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hostID := testID(t, "host")
	u1 := testID(t, "user")
	u2 := testID(t, "user")
	roomID, _ := setupOpenRound(t, store, hostID, 1000, 2, map[string]domain.Tokens{
		u1: 10000,
		u2: 10000,
	})

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	for i := 0; i < Limit; i++ {
		if _, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 1, IdempotencyKey: uuid.NewString()}); err != nil {
			t.Fatalf("Place() u1 attempt %d = %v, want nil", i+1, err)
		}
	}

	_, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 1, IdempotencyKey: uuid.NewString()})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Place() u1 attempt %d error = %v, want ErrRateLimited", Limit+1, err)
	}
	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("Place() error = %v, want *RateLimitError", err)
	}
	if rlErr.RetryAfter <= 0 {
		t.Errorf("RateLimitError.RetryAfter = %v, want > 0", rlErr.RetryAfter)
	}

	// This is the test's 21st wager overall, but a different user's
	// first — it must still succeed, proving the limiter is keyed by
	// user, not globally.
	if _, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u2, Outcome: 0, Amount: 1, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Errorf("Place() u2's first wager = %v, want nil (u1's exhausted limit must not affect u2)", err)
	}
}

func TestPlaceBroadcastsOdds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hostID := testID(t, "host")
	u1 := testID(t, "user")
	roomID, roundID := setupOpenRound(t, store, hostID, 1000, 2, map[string]domain.Tokens{u1: 1000})

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	if _, err := svc.Place(ctx, Request{RoomID: roomID, UserID: u1, Outcome: 0, Amount: 200, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatalf("Place() = %v, want nil", err)
	}

	if len(bc.calls) != 1 {
		t.Fatalf("Broadcast call count = %d, want 1", len(bc.calls))
	}
	if bc.calls[0].roomID != roomID {
		t.Errorf("Broadcast roomID = %q, want %q", bc.calls[0].roomID, roomID)
	}
	env := decodeEnvelope(t, bc.calls[0].payload)
	if env.Type != "odds_updated" {
		t.Errorf("Broadcast envelope Type = %q, want %q", env.Type, "odds_updated")
	}

	var data OddsEvent
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode odds_updated data: %v", err)
	}
	if data.RoundID != roundID {
		t.Errorf("OddsEvent.RoundID = %q, want %q", data.RoundID, roundID)
	}
	wantPools := []int64{200, 0}
	if len(data.Pools) != 2 || data.Pools[0] != wantPools[0] || data.Pools[1] != wantPools[1] {
		t.Errorf("OddsEvent.Pools = %v, want %v", data.Pools, wantPools)
	}
	if data.Total != 200 {
		t.Errorf("OddsEvent.Total = %d, want 200", data.Total)
	}
	if data.Bettors != 1 {
		t.Errorf("OddsEvent.Bettors = %d, want 1", data.Bettors)
	}
	if data.Players != 1 {
		t.Errorf("OddsEvent.Players = %d, want 1", data.Players)
	}
	wantMult := domain.Multipliers(200, []domain.Tokens{200, 0})
	for i := range wantMult {
		if data.Multipliers[i] != wantMult[i] {
			t.Errorf("OddsEvent.Multipliers[%d] = %v, want %v", i, data.Multipliers[i], wantMult[i])
		}
	}

	// Anonymity invariant: the payload's key set must be exactly these
	// six fields — no per-user field can ever sneak in before the round
	// resolves (CLAUDE.md).
	var asMap map[string]any
	if err := json.Unmarshal(env.Data, &asMap); err != nil {
		t.Fatalf("decode odds_updated as map: %v", err)
	}
	wantKeys := map[string]bool{"round_id": true, "pools": true, "total": true, "multipliers": true, "bettors": true, "players": true}
	if len(asMap) != len(wantKeys) {
		t.Fatalf("odds_updated key set = %v, want exactly %v", asMap, wantKeys)
	}
	for k := range asMap {
		if !wantKeys[k] {
			t.Errorf("odds_updated has unexpected key %q", k)
		}
	}
}
