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
// exactly what was announced to a room (or that nothing was).
type stubBroadcaster struct {
	mu    sync.Mutex
	calls []broadcastCall
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
