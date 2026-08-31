package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/round"
	"github.com/zorojuro12/call_it/backend/internal/wager"
)

// Inbound message types this router dispatches.
const (
	TypeCreateRound  = "create_round"
	TypePlaceWager   = "place_wager"
	TypeResolveRound = "resolve_round"
)

// roundService and wagerService are the subset of internal/round's and
// internal/wager's Service methods this router needs, declared here so
// a test double can satisfy them without a live Redis-backed Service.
// *round.Service and *wager.Service satisfy these.
type roundService interface {
	Open(ctx context.Context, roomID, callerID string, spec round.Spec) (round.Opened, error)
	Resolve(ctx context.Context, roomID, callerID string, winningOutcome int) (domain.Settlement, error)
}

type wagerService interface {
	Place(ctx context.Context, req wager.Request) (wager.Accepted, error)
}

// Router is the ws.MessageHandler that dispatches inbound socket
// messages to the round and wager services. The room ID always comes
// from the client's verified token claim (c.RoomID), never from the
// message payload — a payload naming a different room is ignored.
type Router struct {
	rounds roundService
	wagers wagerService
}

// NewRouter constructs a Router bound to rounds and wagers.
func NewRouter(rounds *round.Service, wagers *wager.Service) *Router {
	return &Router{rounds: rounds, wagers: wagers}
}

type createRoundPayload struct {
	Question string   `json:"question"`
	Outcomes []string `json:"outcomes"`
	LockInMS int64    `json:"lock_in_ms"`
}

type placeWagerPayload struct {
	Outcome        int    `json:"outcome"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

type resolveRoundPayload struct {
	WinningOutcome int `json:"winning_outcome"`
}

// Handle satisfies ws.MessageHandler, routing e by its Type.
func (r *Router) Handle(c *Client, e Envelope) {
	switch e.Type {
	case TypeCreateRound:
		var p createRoundPayload
		if err := json.Unmarshal(e.Data, &p); err != nil {
			r.replyError(c, "malformed", "malformed create_round payload")
			return
		}
		_, err := r.rounds.Open(context.Background(), c.RoomID, c.UserID, round.Spec{
			Question: p.Question,
			Outcomes: p.Outcomes,
			LockIn:   time.Duration(p.LockInMS) * time.Millisecond,
		})
		if err != nil {
			r.replyServiceError(c, err)
		}

	case TypePlaceWager:
		var p placeWagerPayload
		if err := json.Unmarshal(e.Data, &p); err != nil {
			r.replyError(c, "malformed", "malformed place_wager payload")
			return
		}
		accepted, err := r.wagers.Place(context.Background(), wager.Request{
			RoomID:         c.RoomID,
			UserID:         c.UserID,
			Outcome:        p.Outcome,
			Amount:         domain.Tokens(p.Amount),
			IdempotencyKey: p.IdempotencyKey,
		})
		if err != nil {
			r.replyServiceError(c, err)
			return
		}
		c.Send(mustEncode(TypeWagerAccepted, WagerAcceptedEvent{
			RoundID: accepted.RoundID,
			Outcome: p.Outcome,
			Amount:  p.Amount,
			Balance: int64(accepted.Balance),
		}))

	case TypeResolveRound:
		var p resolveRoundPayload
		if err := json.Unmarshal(e.Data, &p); err != nil {
			r.replyError(c, "malformed", "malformed resolve_round payload")
			return
		}
		_, err := r.rounds.Resolve(context.Background(), c.RoomID, c.UserID, p.WinningOutcome)
		if err != nil {
			r.replyServiceError(c, err)
		}

	default:
		r.replyError(c, "unknown_type", "unsupported message type: "+e.Type)
	}
}

// replyError sends a private error envelope to the sender only — an
// error belongs to the sender, not the room.
func (r *Router) replyError(c *Client, code, message string) {
	c.Send(mustEncode(TypeError, ErrorEvent{Code: code, Message: message}))
}

// replyServiceError maps a round/wager service error to a stable
// client-facing code and replies privately. An unrecognized error
// becomes a generic internal_error — leaking an internal error string
// to a client is the information disclosure internal/httpapi's error
// envelope already avoids (Phase 3); the real error is logged instead.
func (r *Router) replyServiceError(c *Client, err error) {
	switch {
	case errors.Is(err, redisstore.ErrHostCannotBet):
		r.replyError(c, "host_cannot_bet", err.Error())
	case errors.Is(err, redisstore.ErrPoolLocked):
		r.replyError(c, "pool_locked", err.Error())
	case errors.Is(err, redisstore.ErrNotInRoom):
		r.replyError(c, "not_in_room", err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		r.replyError(c, "insufficient_funds", err.Error())
	case errors.Is(err, domain.ErrInvalidOutcome):
		r.replyError(c, "invalid_outcome", err.Error())
	case errors.Is(err, round.ErrNotHost):
		r.replyError(c, "not_host", err.Error())
	case errors.Is(err, round.ErrInvalidSpec):
		r.replyError(c, "invalid_spec", err.Error())
	case errors.Is(err, round.ErrRoundInProgress):
		r.replyError(c, "round_in_progress", err.Error())
	case errors.Is(err, wager.ErrNoActiveRound):
		r.replyError(c, "no_active_round", err.Error())
	case errors.Is(err, wager.ErrBadIdempotency):
		r.replyError(c, "bad_idempotency_key", err.Error())
	case errors.Is(err, wager.ErrRateLimited):
		msg := err.Error()
		var rlErr *wager.RateLimitError
		if errors.As(err, &rlErr) {
			msg = fmt.Sprintf("%s (retry after %s)", msg, rlErr.RetryAfter)
		}
		r.replyError(c, "rate_limited", msg)
	default:
		log.Printf("ws: router: unmapped service error: %v", err)
		r.replyError(c, "internal_error", "an internal error occurred")
	}
}
