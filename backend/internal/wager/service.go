// Package wager validates and places wagers: resolve the room's current
// round, call internal/redisstore's atomic Lua writer, then report the
// wagerer's new anonymous state. It never computes a balance or payout
// itself — place_wager.lua is the sole authority.
package wager

import (
	"context"
	"errors"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/round"
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
	Balance     domain.Tokens
	Pools       []domain.Tokens
	Total       domain.Tokens
	Multipliers []float64
	Bettors     int
	Players     int
}

// Service places wagers and reports the wagerer's new state.
type Service struct {
	store       *redisstore.Store
	broadcaster round.Broadcaster
}

// NewService constructs a Service bound to store and b.
func NewService(store *redisstore.Store, b round.Broadcaster) *Service {
	return &Service{store: store, broadcaster: b}
}

// Place validates and places a wager, then reports the wagerer's new
// state. No Go-side balance or lockout check — place_wager.lua is the
// authority on both.
func (s *Service) Place(ctx context.Context, req Request) (Accepted, error) {
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

	return Accepted{
		Balance:     result.Balance,
		Pools:       result.Pools,
		Total:       result.Total,
		Multipliers: domain.Multipliers(result.Total, result.Pools),
		Bettors:     result.BettorCount,
		Players:     players,
	}, nil
}
