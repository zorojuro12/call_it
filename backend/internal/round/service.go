package round

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// MinLockIn and MaxLockIn bound the host's chosen lock window.
const (
	MinLockIn = 3 * time.Second
	MaxLockIn = 120 * time.Second
)

// Service opens and resolves rounds, and owns each round's server-side
// clock.
type Service struct {
	store       *redisstore.Store
	broadcaster Broadcaster
	refundGrace time.Duration
}

// NewService constructs a Service bound to store and b.
func NewService(store *redisstore.Store, b Broadcaster) *Service {
	return &Service{store: store, broadcaster: b, refundGrace: RefundGrace}
}

// Open opens a round in roomID on callerID's behalf, persists it, then
// broadcasts round_opened to the room. Persist-before-broadcast is
// deliberate: no client should ever learn of a round Redis does not
// have.
func (s *Service) Open(ctx context.Context, roomID, callerID string, spec Spec) (Opened, error) {
	rm, err := s.store.Room(ctx, roomID)
	if err != nil {
		return Opened{}, err
	}
	if rm.HostID != callerID {
		return Opened{}, ErrNotHost
	}

	spec.Question = strings.TrimSpace(spec.Question)
	if spec.Question == "" {
		return Opened{}, ErrInvalidSpec
	}
	if err := domain.ValidateOutcomeCount(len(spec.Outcomes)); err != nil {
		return Opened{}, ErrInvalidSpec
	}
	outcomes := make([]string, len(spec.Outcomes))
	for i, o := range spec.Outcomes {
		outcomes[i] = strings.TrimSpace(o)
		if outcomes[i] == "" {
			return Opened{}, ErrInvalidSpec
		}
	}
	spec.Outcomes = outcomes
	if spec.LockIn < MinLockIn || spec.LockIn > MaxLockIn {
		return Opened{}, ErrInvalidSpec
	}

	if currentID, err := s.store.CurrentRound(ctx, roomID); err == nil {
		current, err := s.store.Round(ctx, currentID)
		if err != nil {
			return Opened{}, err
		}
		if !current.Status.IsTerminal() {
			return Opened{}, ErrRoundInProgress
		}
	} else if !errors.Is(err, redisstore.ErrNotFound) {
		return Opened{}, err
	}

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

	go s.watch(context.Background(), roomID, roundID, lockAt)

	return opened, nil
}
