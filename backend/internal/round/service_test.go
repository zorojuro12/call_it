package round

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// stubBroadcaster records every Broadcast call, so tests can assert
// exactly what was announced to a room (or that nothing was). names
// lets a test stand in for connected clients' display names, read by
// Names.
type stubBroadcaster struct {
	mu    sync.Mutex
	calls []broadcastCall
	names map[string]string
}

type broadcastCall struct {
	roomID  string
	payload []byte
}

func (b *stubBroadcaster) Broadcast(roomID string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, broadcastCall{roomID: roomID, payload: payload})
}

func (b *stubBroadcaster) Names(roomID string) map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.names == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(b.names))
	for k, v := range b.names {
		out[k] = v
	}
	return out
}

func (b *stubBroadcaster) Calls() []broadcastCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]broadcastCall, len(b.calls))
	copy(out, b.calls)
	return out
}

// decodeEnvelope decodes a broadcast payload's {"type":...,"data":...}
// shape without depending on internal/ws (round must not import it —
// see Broadcaster's doc comment).
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

func TestOpen(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	spec := Spec{Question: "Clutch?", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}
	before := time.Now()
	opened, err := svc.Open(ctx, roomID, "host1", spec)
	if err != nil {
		t.Fatalf("Open() = %v, want nil", err)
	}

	if _, err := uuid.Parse(opened.RoundID); err != nil {
		t.Errorf("Open().RoundID = %q, not a valid UUID: %v", opened.RoundID, err)
	}
	if opened.Question != spec.Question {
		t.Errorf("Open().Question = %q, want %q", opened.Question, spec.Question)
	}
	if len(opened.Outcomes) != 2 || opened.Outcomes[0] != "Yes" || opened.Outcomes[1] != "No" {
		t.Errorf("Open().Outcomes = %v, want %v", opened.Outcomes, spec.Outcomes)
	}
	wantLockAt := before.Add(spec.LockIn)
	gotLockAt := time.UnixMilli(opened.LockAtMS)
	if diff := gotLockAt.Sub(wantLockAt); diff < -1*time.Second || diff > 1*time.Second {
		t.Errorf("Open().LockAtMS = %v, want within 1s of %v", gotLockAt, wantLockAt)
	}

	current, err := store.CurrentRound(ctx, roomID)
	if err != nil {
		t.Fatalf("CurrentRound() = %v, want nil", err)
	}
	if current != opened.RoundID {
		t.Errorf("CurrentRound() = %q, want %q", current, opened.RoundID)
	}
	rd, err := store.Round(ctx, opened.RoundID)
	if err != nil {
		t.Fatalf("Round() = %v, want nil", err)
	}
	if rd.Status != domain.RoundOpen {
		t.Errorf("Round().Status = %q, want %q", rd.Status, domain.RoundOpen)
	}

	calls := bc.Calls()
	if len(calls) != 1 {
		t.Fatalf("Broadcast call count = %d, want 1", len(calls))
	}
	if calls[0].roomID != roomID {
		t.Errorf("Broadcast roomID = %q, want %q", calls[0].roomID, roomID)
	}
	env := decodeEnvelope(t, calls[0].payload)
	if env.Type != "round_opened" {
		t.Errorf("Broadcast envelope Type = %q, want %q", env.Type, "round_opened")
	}
	var data Opened
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode round_opened data: %v", err)
	}
	if data.RoundID != opened.RoundID || data.Question != spec.Question || data.LockAtMS != opened.LockAtMS {
		t.Errorf("Broadcast data = %+v, want it to echo %+v", data, opened)
	}
}

func TestOpenNotHost(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if _, err := store.JoinRoom(ctx, roomID, "u2", 1000); err != nil {
		t.Fatalf("JoinRoom() = %v, want nil", err)
	}

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	spec := Spec{Question: "Clutch?", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}
	_, err := svc.Open(ctx, roomID, "u2", spec)
	if !errors.Is(err, ErrNotHost) {
		t.Fatalf("Open() by non-host error = %v, want ErrNotHost", err)
	}

	if _, err := store.CurrentRound(ctx, roomID); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("CurrentRound() after rejected Open error = %v, want ErrNotFound", err)
	}
	if len(bc.Calls()) != 0 {
		t.Errorf("Broadcast call count = %d, want 0", len(bc.Calls()))
	}
}

func TestOpenInvalid(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
	}{
		{"empty question", Spec{Question: "   ", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}},
		{"too few outcomes", Spec{Question: "Q?", Outcomes: []string{"Yes"}, LockIn: 10 * time.Second}},
		{"too many outcomes", Spec{Question: "Q?", Outcomes: []string{"A", "B", "C", "D", "E"}, LockIn: 10 * time.Second}},
		{"blank outcome label", Spec{Question: "Q?", Outcomes: []string{"Yes", "  "}, LockIn: 10 * time.Second}},
		{"lock window too short", Spec{Question: "Q?", Outcomes: []string{"Yes", "No"}, LockIn: 1 * time.Second}},
		{"lock window too long", Spec{Question: "Q?", Outcomes: []string{"Yes", "No"}, LockIn: 5 * time.Minute}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			roomID := testID(t, "room")
			if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
				t.Fatalf("CreateRoom() = %v, want nil", err)
			}
			bc := &stubBroadcaster{}
			svc := NewService(store, bc)

			_, err := svc.Open(ctx, roomID, "host1", tt.spec)
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("Open() error = %v, want ErrInvalidSpec", err)
			}
			if _, err := store.CurrentRound(ctx, roomID); !errors.Is(err, redisstore.ErrNotFound) {
				t.Errorf("CurrentRound() error = %v, want ErrNotFound", err)
			}
			if len(bc.Calls()) != 0 {
				t.Errorf("Broadcast call count = %d, want 0", len(bc.Calls()))
			}
		})
	}
}

func TestOpenConcurrent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	spec := Spec{Question: "First?", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}
	first, err := svc.Open(ctx, roomID, "host1", spec)
	if err != nil {
		t.Fatalf("Open() first = %v, want nil", err)
	}

	spec2 := Spec{Question: "Second?", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}
	_, err = svc.Open(ctx, roomID, "host1", spec2)
	if !errors.Is(err, ErrRoundInProgress) {
		t.Fatalf("Open() second error = %v, want ErrRoundInProgress", err)
	}

	current, err := store.CurrentRound(ctx, roomID)
	if err != nil {
		t.Fatalf("CurrentRound() = %v, want nil", err)
	}
	if current != first.RoundID {
		t.Errorf("CurrentRound() = %q, want %q (the first round)", current, first.RoundID)
	}
}

func TestResolve(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	roomID := testID(t, "room")
	if err := store.CreateRoom(ctx, roomID, testID(t, "code"), "host1", 1000); err != nil {
		t.Fatalf("CreateRoom() = %v, want nil", err)
	}
	if _, err := store.JoinRoom(ctx, roomID, "u1", 1000); err != nil {
		t.Fatalf("JoinRoom(u1) = %v, want nil", err)
	}
	if _, err := store.JoinRoom(ctx, roomID, "u2", 1000); err != nil {
		t.Fatalf("JoinRoom(u2) = %v, want nil", err)
	}

	bc := &stubBroadcaster{}
	svc := NewService(store, bc)

	spec := Spec{Question: "Q?", Outcomes: []string{"Yes", "No"}, LockIn: 10 * time.Second}
	opened, err := svc.Open(ctx, roomID, "host1", spec)
	if err != nil {
		t.Fatalf("Open() = %v, want nil", err)
	}

	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: opened.RoundID, UserID: "u1",
		Outcome: 0, Amount: 100, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager(u1) = %v, want nil", err)
	}
	if _, err := store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID: roomID, RoundID: opened.RoundID, UserID: "u2",
		Outcome: 1, Amount: 100, IdempotencyKey: testID(t, "idem"),
	}); err != nil {
		t.Fatalf("PlaceWager(u2) = %v, want nil", err)
	}

	if err := store.LockRound(ctx, opened.RoundID); err != nil {
		t.Fatalf("LockRound() = %v, want nil", err)
	}

	settlement, err := svc.Resolve(ctx, roomID, "host1", 0)
	if err != nil {
		t.Fatalf("Resolve() = %v, want nil", err)
	}

	var u1Result, u2Result *domain.PlayerResult
	for i := range settlement.Results {
		switch settlement.Results[i].UserID {
		case "u1":
			u1Result = &settlement.Results[i]
		case "u2":
			u2Result = &settlement.Results[i]
		}
	}
	if u1Result == nil || u1Result.Returned != 200 {
		t.Errorf("u1 result = %+v, want Returned 200", u1Result)
	}
	if u2Result == nil || u2Result.Returned != 0 {
		t.Errorf("u2 result = %+v, want Returned 0", u2Result)
	}
	if settlement.Dust != 0 {
		t.Errorf("Settlement.Dust = %d, want 0", settlement.Dust)
	}

	if _, err := store.CurrentRound(ctx, roomID); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("CurrentRound() after Resolve error = %v, want ErrNotFound", err)
	}

	calls := bc.Calls()
	var resolvedCall *broadcastCall
	for i := range calls {
		env := decodeEnvelope(t, calls[i].payload)
		if env.Type == "round_resolved" {
			resolvedCall = &calls[i]
		}
	}
	if resolvedCall == nil {
		t.Fatalf("no round_resolved broadcast found in %+v", calls)
	}
	env := decodeEnvelope(t, resolvedCall.payload)
	var data ResolvedEvent
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode round_resolved data: %v", err)
	}
	if len(data.Results) != 2 {
		t.Fatalf("ResolvedEvent.Results has %d rows, want 2", len(data.Results))
	}
	var u1Row, u2Row *ResultRow
	for i := range data.Results {
		switch data.Results[i].UserID {
		case "u1":
			u1Row = &data.Results[i]
		case "u2":
			u2Row = &data.Results[i]
		}
	}
	if u1Row == nil || u1Row.Staked != 100 || u1Row.Returned != 200 || u1Row.Net != 100 {
		t.Errorf("u1 row = %+v, want Staked 100, Returned 200, Net 100", u1Row)
	}
	// The loser appears too — this event is the first and only moment
	// per-player stakes are revealed.
	if u2Row == nil || u2Row.Staked != 100 || u2Row.Returned != 0 || u2Row.Net != -100 {
		t.Errorf("u2 row = %+v, want Staked 100, Returned 0, Net -100", u2Row)
	}
}
