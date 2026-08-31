// Package wager validates and places wagers: resolve the room's current
// round, call internal/redisstore's atomic Lua writer, then report the
// wagerer's new anonymous state. It never computes a balance or payout
// itself — place_wager.lua is the sole authority.
package wager

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/round"
)

// Scope, Limit, and Window bound how often one user may place a wager.
// This is the shared sliding-window limiter (redisstore.Store.Allow /
// rate_limit.lua) — CLAUDE.md's "one limiter, every call site" — not a
// second implementation.
const (
	Scope  = "wager"
	Limit  = 20
	Window = 10 * time.Second
)

// Request is what a caller supplies to Place. RoundID may be left
// empty — Place resolves it from the room's current-round index.
type Request struct {
	RoomID, RoundID, UserID string
	Outcome                 int
	Amount                  domain.Tokens
	IdempotencyKey          string
}

// Accepted is the wagerer's new anonymous state after a successful
// Place — never another player's stake.
type Accepted struct {
	RoundID     string
	Balance     domain.Tokens
	Pools       []domain.Tokens
	Total       domain.Tokens
	Multipliers []float64
	Bettors     int
	Players     int
}

// Recorder observes one latency sample. Satisfied structurally by
// *metrics.Histogram — internal/wager does not import internal/metrics.
type Recorder interface {
	Observe(d time.Duration)
}

// Service places wagers and reports the wagerer's new state.
type Service struct {
	store       *redisstore.Store
	broadcaster round.Broadcaster
	okLatency   Recorder
	errLatency  Recorder
}

// NewService constructs a Service bound to store and b. ok records the
// latency of a successful Place; failed records the latency of a
// rejected one. Either may be nil, which disables that recording.
func NewService(store *redisstore.Store, b round.Broadcaster, ok, failed Recorder) *Service {
	return &Service{store: store, broadcaster: b, okLatency: ok, errLatency: failed}
}

// Place validates and places a wager, then reports the wagerer's new
// state. No Go-side balance or lockout check — place_wager.lua is the
// authority on both.
func (s *Service) Place(ctx context.Context, req Request) (accepted Accepted, err error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		if err != nil {
			if s.errLatency != nil {
				s.errLatency.Observe(elapsed)
			}
			return
		}
		if s.okLatency != nil {
			s.okLatency.Observe(elapsed)
		}
	}()

	decision, err := s.store.Allow(ctx, Scope, req.UserID, Limit, Window)
	if err != nil {
		return Accepted{}, err
	}
	if !decision.Allowed {
		return Accepted{}, &RateLimitError{RetryAfter: decision.RetryAfter}
	}

	parsed, err := uuid.Parse(req.IdempotencyKey)
	if err != nil || parsed.Version() != 4 {
		return Accepted{}, ErrBadIdempotency
	}

	roundID := req.RoundID
	if roundID == "" {
		var err error
		roundID, err = s.store.CurrentRound(ctx, req.RoomID)
		if errors.Is(err, redisstore.ErrNotFound) {
			return Accepted{}, ErrNoActiveRound
		}
		if err != nil {
			return Accepted{}, err
		}
	}

	// A zero/negative stake, or one exceeding the wagerer's session
	// balance, is rejected here so it never costs a Redis round trip —
	// but only when a balance is available to check against. A user
	// with no wallet in this room falls through to place_wager.lua,
	// which is the authority on ErrNotInRoom.
	if balance, err := s.store.Balance(ctx, req.RoomID, req.UserID); err == nil {
		if err := domain.ValidateStake(req.Amount, balance); err != nil {
			return Accepted{}, err
		}
	} else if !errors.Is(err, redisstore.ErrNotFound) {
		return Accepted{}, err
	}

	result, err := s.store.PlaceWager(ctx, redisstore.WagerRequest{
		RoomID:         req.RoomID,
		RoundID:        roundID,
		UserID:         req.UserID,
		Outcome:        req.Outcome,
		Amount:         req.Amount,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return Accepted{}, err
	}

	players, err := s.store.PlayerCount(ctx, req.RoomID)
	if err != nil {
		return Accepted{}, err
	}

	accepted = Accepted{
		RoundID:     roundID,
		Balance:     result.Balance,
		Pools:       result.Pools,
		Total:       result.Total,
		Multipliers: domain.Multipliers(result.Total, result.Pools),
		Bettors:     result.BettorCount,
		Players:     players,
	}

	// Broadcast after the Lua call succeeds, never before — an
	// announced wager that then failed would desynchronize every
	// client's odds from Redis.
	pools := make([]int64, len(result.Pools))
	for i, p := range result.Pools {
		pools[i] = int64(p)
	}
	payload, err := round.EncodeEnvelope("odds_updated", OddsEvent{
		RoundID:     roundID,
		Pools:       pools,
		Total:       int64(result.Total),
		Multipliers: accepted.Multipliers,
		Bettors:     accepted.Bettors,
		Players:     accepted.Players,
	})
	if err != nil {
		return Accepted{}, err
	}
	s.broadcaster.Broadcast(req.RoomID, payload)

	return accepted, nil
}
