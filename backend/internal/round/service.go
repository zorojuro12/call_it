package round

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// Service opens and resolves rounds, and owns each round's server-side
// clock.
type Service struct {
	store       *redisstore.Store
	broadcaster Broadcaster
}

// NewService constructs a Service bound to store and b.
func NewService(store *redisstore.Store, b Broadcaster) *Service {
	return &Service{store: store, broadcaster: b}
}

// Open opens a round in roomID on callerID's behalf, persists it, then
// broadcasts round_opened to the room. Persist-before-broadcast is
// deliberate: no client should ever learn of a round Redis does not
// have.
func (s *Service) Open(ctx context.Context, roomID, callerID string, spec Spec) (Opened, error) {
	roundID := uuid.NewString()
	lockAt := time.Now().Add(spec.LockIn)

	if err := s.store.CreateRound(ctx, roundID, roomID, spec.Question, spec.Outcomes, lockAt); err != nil {
		return Opened{}, err
	}

	opened := Opened{
		RoundID:  roundID,
		Question: spec.Question,
		Outcomes: spec.Outcomes,
		LockAtMS: lockAt.UnixMilli(),
	}

	payload, err := EncodeEnvelope("round_opened", opened)
	if err != nil {
		return Opened{}, err
	}
	s.broadcaster.Broadcast(roomID, payload)

	return opened, nil
}
