package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/round"
	"github.com/zorojuro12/call_it/backend/internal/wager"
)

var errUnrecognizedForTest = errors.New("ws: an error the router's table does not name")

// stubRoundService and stubWagerService satisfy roundService and
// wagerService without a live Redis-backed Service, and record every
// call so tests can assert exactly what the router passed through.
type stubRoundService struct {
	openRoomID, openCallerID string
	openSpec                 round.Spec
	openErr                  error

	resolveRoomID, resolveCallerID string
	resolveOutcome                 int
	resolveErr                     error
}

func (s *stubRoundService) Open(ctx context.Context, roomID, callerID string, spec round.Spec) (round.Opened, error) {
	s.openRoomID, s.openCallerID, s.openSpec = roomID, callerID, spec
	return round.Opened{}, s.openErr
}

func (s *stubRoundService) Resolve(ctx context.Context, roomID, callerID string, winningOutcome int) (domain.Settlement, error) {
	s.resolveRoomID, s.resolveCallerID, s.resolveOutcome = roomID, callerID, winningOutcome
	return domain.Settlement{}, s.resolveErr
}

type stubWagerService struct {
	placeReq    wager.Request
	placeErr    error
	placeResult wager.Accepted
	placed      bool
}

func (s *stubWagerService) Place(ctx context.Context, req wager.Request) (wager.Accepted, error) {
	s.placeReq = req
	s.placed = true
	return s.placeResult, s.placeErr
}

func testClient(userID, roomID string) *Client {
	c := NewClient(nil, Identity{UserID: userID, DisplayName: "Ada"}, DefaultClientConfig())
	c.RoomID = roomID
	return c
}

func TestRouterDispatch(t *testing.T) {
	t.Run("create_round routes to rounds.Open", func(t *testing.T) {
		rounds := &stubRoundService{}
		wagers := &stubWagerService{}
		r := &Router{rounds: rounds, wagers: wagers}
		c := testClient("u1", "room1")

		data, _ := json.Marshal(createRoundPayload{Question: "Q?", Outcomes: []string{"Yes", "No"}, LockInMS: 10000})
		r.Handle(c, Envelope{Type: TypeCreateRound, Data: data})

		if rounds.openRoomID != "room1" || rounds.openCallerID != "u1" {
			t.Errorf("Open called with (%q, %q), want (%q, %q)", rounds.openRoomID, rounds.openCallerID, "room1", "u1")
		}
		if rounds.openSpec.Question != "Q?" || len(rounds.openSpec.Outcomes) != 2 {
			t.Errorf("Open spec = %+v, want Question Q? and 2 outcomes", rounds.openSpec)
		}
	})

	t.Run("place_wager routes to wagers.Place, ignoring a payload room_id", func(t *testing.T) {
		rounds := &stubRoundService{}
		wagers := &stubWagerService{}
		r := &Router{rounds: rounds, wagers: wagers}
		c := testClient("u1", "room1")

		data, _ := json.Marshal(map[string]any{
			"room_id":         "someone-elses-room",
			"outcome":         1,
			"amount":          200,
			"idempotency_key": "11111111-1111-4111-8111-111111111111",
		})
		r.Handle(c, Envelope{Type: TypePlaceWager, Data: data})

		if !wagers.placed {
			t.Fatal("Place was not called")
		}
		if wagers.placeReq.RoomID != "room1" {
			t.Errorf("Place RoomID = %q, want %q (the client's own room, not the payload's)", wagers.placeReq.RoomID, "room1")
		}
		if wagers.placeReq.UserID != "u1" || wagers.placeReq.Outcome != 1 || wagers.placeReq.Amount != 200 {
			t.Errorf("Place req = %+v, want UserID u1, Outcome 1, Amount 200", wagers.placeReq)
		}
	})

	t.Run("resolve_round routes to rounds.Resolve", func(t *testing.T) {
		rounds := &stubRoundService{}
		wagers := &stubWagerService{}
		r := &Router{rounds: rounds, wagers: wagers}
		c := testClient("host1", "room1")

		data, _ := json.Marshal(resolveRoundPayload{WinningOutcome: 2})
		r.Handle(c, Envelope{Type: TypeResolveRound, Data: data})

		if rounds.resolveRoomID != "room1" || rounds.resolveCallerID != "host1" || rounds.resolveOutcome != 2 {
			t.Errorf("Resolve called with (%q, %q, %d), want (%q, %q, 2)", rounds.resolveRoomID, rounds.resolveCallerID, rounds.resolveOutcome, "room1", "host1")
		}
	})
}

func TestWagerAccepted(t *testing.T) {
	t.Run("a successful wager privately reports the placer's new balance", func(t *testing.T) {
		// Arrange
		wagers := &stubWagerService{placeResult: wager.Accepted{RoundID: "rd1", Balance: 900}}
		rounds := &stubRoundService{}
		r := &Router{rounds: rounds, wagers: wagers}
		c := testClient("u1", "room1")

		data, _ := json.Marshal(placeWagerPayload{Outcome: 0, Amount: 100, IdempotencyKey: "11111111-1111-4111-8111-111111111111"})

		// Act
		r.Handle(c, Envelope{Type: TypePlaceWager, Data: data})

		// Assert
		select {
		case payload := <-c.send:
			env, err := Decode(payload)
			if err != nil {
				t.Fatalf("Decode() = %v, want nil", err)
			}
			if env.Type != TypeWagerAccepted {
				t.Fatalf("Type = %q, want %q", env.Type, TypeWagerAccepted)
			}
			var ev WagerAcceptedEvent
			if err := json.Unmarshal(env.Data, &ev); err != nil {
				t.Fatalf("decode WagerAcceptedEvent: %v", err)
			}
			want := WagerAcceptedEvent{RoundID: "rd1", Outcome: 0, Amount: 100, Balance: 900}
			if ev != want {
				t.Errorf("WagerAcceptedEvent = %+v, want %+v", ev, want)
			}
		default:
			t.Fatal("no wager_accepted reply sent to the client's send channel")
		}

		select {
		case payload := <-c.send:
			t.Fatalf("unexpected second message sent: %s", payload)
		default:
		}
	})

	t.Run("a failed wager sends only an error, never wager_accepted", func(t *testing.T) {
		// Arrange
		wagers := &stubWagerService{placeErr: domain.ErrInsufficientFunds}
		rounds := &stubRoundService{}
		r := &Router{rounds: rounds, wagers: wagers}
		c := testClient("u1", "room1")

		data, _ := json.Marshal(placeWagerPayload{Outcome: 0, Amount: 100, IdempotencyKey: "11111111-1111-4111-8111-111111111111"})

		// Act
		r.Handle(c, Envelope{Type: TypePlaceWager, Data: data})

		// Assert
		select {
		case payload := <-c.send:
			env, err := Decode(payload)
			if err != nil {
				t.Fatalf("Decode() = %v, want nil", err)
			}
			if env.Type != TypeError {
				t.Fatalf("Type = %q, want %q", env.Type, TypeError)
			}
		default:
			t.Fatal("no error reply sent to the client's send channel")
		}

		select {
		case payload := <-c.send:
			t.Fatalf("unexpected second message sent: %s", payload)
		default:
		}
	})
}

func TestRouterErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"host cannot bet", redisstore.ErrHostCannotBet, "host_cannot_bet"},
		{"pool locked", redisstore.ErrPoolLocked, "pool_locked"},
		{"not in room", redisstore.ErrNotInRoom, "not_in_room"},
		{"insufficient funds", domain.ErrInsufficientFunds, "insufficient_funds"},
		{"invalid outcome", domain.ErrInvalidOutcome, "invalid_outcome"},
		{"not host", round.ErrNotHost, "not_host"},
		{"round in progress", round.ErrRoundInProgress, "round_in_progress"},
		{"no active round", wager.ErrNoActiveRound, "no_active_round"},
		{"bad idempotency key", wager.ErrBadIdempotency, "bad_idempotency_key"},
		{"rate limited", &wager.RateLimitError{RetryAfter: 5 * time.Second}, "rate_limited"},
		{"unrecognized error", errUnrecognizedForTest, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wagers := &stubWagerService{placeErr: tt.err}
			rounds := &stubRoundService{}
			r := &Router{rounds: rounds, wagers: wagers}
			c := testClient("u1", "room1")

			data, _ := json.Marshal(placeWagerPayload{Outcome: 0, Amount: 50, IdempotencyKey: "11111111-1111-4111-8111-111111111111"})
			r.Handle(c, Envelope{Type: TypePlaceWager, Data: data})

			select {
			case payload := <-c.send:
				env, err := Decode(payload)
				if err != nil {
					t.Fatalf("Decode() = %v, want nil", err)
				}
				if env.Type != TypeError {
					t.Fatalf("Type = %q, want %q", env.Type, TypeError)
				}
				var ev ErrorEvent
				if err := json.Unmarshal(env.Data, &ev); err != nil {
					t.Fatalf("decode ErrorEvent: %v", err)
				}
				if ev.Code != tt.code {
					t.Errorf("Code = %q, want %q", ev.Code, tt.code)
				}
			default:
				t.Fatal("no error reply sent to the client's send channel")
			}
		})
	}
}
