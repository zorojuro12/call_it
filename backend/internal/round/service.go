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
	ctx         context.Context
	store       *redisstore.Store
	broadcaster Broadcaster
	refundGrace time.Duration
}

// NewService constructs a Service bound to store and b. ctx is the
// base context every round's server-side timer runs against — not a
// per-request one, so a disconnecting host or a finished HTTP/WS
// request never cancels a round's clock. Cancel ctx to stop every
// in-flight timer, e.g. on process shutdown.
func NewService(ctx context.Context, store *redisstore.Store, b Broadcaster) *Service {
	return &Service{ctx: ctx, store: store, broadcaster: b, refundGrace: RefundGrace}
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

	go s.watch(s.ctx, roomID, roundID, lockAt)

	return opened, nil
}

// Resolve settles roomID's current round on callerID's (the host's)
// behalf, then reveals every player's result to the room. This is the
// first and only moment a per-player stake is disclosed (CLAUDE.md) —
// settlement math itself lives entirely in domain.Settle, reached only
// through store.SettleRound; nothing here recomputes a payout.
func (s *Service) Resolve(ctx context.Context, roomID, callerID string, winningOutcome int) (domain.Settlement, error) {
	rm, err := s.store.Room(ctx, roomID)
	if err != nil {
		return domain.Settlement{}, err
	}
	if rm.HostID != callerID {
		return domain.Settlement{}, ErrNotHost
	}

	roundID, err := s.store.CurrentRound(ctx, roomID)
	if errors.Is(err, redisstore.ErrNotFound) {
		return domain.Settlement{}, ErrNoActiveRound
	}
	if err != nil {
		return domain.Settlement{}, err
	}

	rd, err := s.store.Round(ctx, roundID)
	if err != nil {
		return domain.Settlement{}, err
	}
	if err := domain.ValidateOutcomeIndex(winningOutcome, rd.OutcomeCount); err != nil {
		return domain.Settlement{}, err
	}

	// On any error past this point, the current-round index is left
	// alone — a failed resolve must leave the round resolvable again.
	settlement, err := s.store.SettleRound(ctx, roundID, winningOutcome, uuid.NewString())
	if err != nil {
		return domain.Settlement{}, err
	}

	if err := s.store.ClearCurrentRound(ctx, roomID); err != nil {
		return domain.Settlement{}, err
	}

	names := s.broadcaster.Names(roomID)
	results := make([]ResultRow, len(settlement.Results))
	for i, r := range settlement.Results {
		name := names[r.UserID]
		if name == "" {
			// A player who disconnected before resolution has no name
			// available — their payout is still real, so the row
			// falls back to their user ID rather than being dropped.
			name = r.UserID
		}
		results[i] = ResultRow{
			UserID:      r.UserID,
			DisplayName: name,
			Staked:      int64(r.Staked),
			Returned:    int64(r.Returned),
			Net:         int64(r.Net),
		}
	}

	payload, err := EncodeEnvelope("round_resolved", ResolvedEvent{
		RoundID:        roundID,
		WinningOutcome: winningOutcome,
		Results:        results,
		Dust:           int64(settlement.Dust),
		Refunded:       settlement.Refunded,
	})
	if err != nil {
		return domain.Settlement{}, err
	}
	s.broadcaster.Broadcast(roomID, payload)

	return settlement, nil
}
