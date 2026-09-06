package redisstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	lua "github.com/zorojuro12/call_it/backend/scripts/lua"
)

var placeWagerScript = redis.NewScript(lua.PlaceWager)

// WagerRequest is everything place_wager.lua needs to accept or reject a
// wager. It carries no timestamp — lockout is judged by Redis's own
// clock inside the script (see Task 4 Checkpoint 2), never a value a
// caller could influence.
type WagerRequest struct {
	RoomID, RoundID, UserID string
	Outcome                 int
	Amount                  domain.Tokens
	IdempotencyKey          string
}

// WagerResult is the anonymity-safe reply from an accepted wager: the
// wagerer's own balance, pool totals, and a distinct-bettor count. No
// field here may ever carry another player's individual wager.
type WagerResult struct {
	Balance     domain.Tokens
	Pools       []domain.Tokens
	Total       domain.Tokens
	BettorCount int
}

// PlaceWager runs place_wager.lua, which go-redis executes via EVALSHA
// with an automatic EVAL fallback on NOSCRIPT — no explicit script
// loading step is needed.
func (s *Store) PlaceWager(ctx context.Context, req WagerRequest) (WagerResult, error) {
	keys := []string{
		RoomKey(req.RoomID),
		RoomWalletsKey(req.RoomID),
		RoundKey(req.RoundID),
		RoundPoolsKey(req.RoundID),
		RoundWagersKey(req.RoundID),
		RoundBettorsKey(req.RoundID),
		IdemKey(req.IdempotencyKey),
		s.outboxStream,
	}
	argv := []interface{}{
		req.UserID,
		strconv.Itoa(req.Outcome),
		strconv.FormatInt(int64(req.Amount), 10),
		req.IdempotencyKey,
		req.RoomID,
		req.RoundID,
	}

	res, err := placeWagerScript.Run(ctx, s.client, keys, argv...).Result()
	if err != nil {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: %w", err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: %w", err)
	}
	if len(reply) == 0 {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: empty reply")
	}

	if reply[0] != "OK" {
		return WagerResult{}, mapWagerStatus(reply)
	}

	return parseWagerReply(reply)
}

func parseWagerReply(reply []string) (WagerResult, error) {
	if len(reply) < 4 {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: malformed reply %v", reply)
	}

	balance, err := strconv.ParseInt(reply[1], 10, 64)
	if err != nil {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: malformed balance %q: %w", reply[1], err)
	}
	bettorCount, err := strconv.Atoi(reply[2])
	if err != nil {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: malformed bettor count %q: %w", reply[2], err)
	}
	total, err := strconv.ParseInt(reply[3], 10, 64)
	if err != nil {
		return WagerResult{}, fmt.Errorf("redisstore: place wager: malformed total %q: %w", reply[3], err)
	}

	pools := make([]domain.Tokens, 0, len(reply)-4)
	for _, p := range reply[4:] {
		amount, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return WagerResult{}, fmt.Errorf("redisstore: place wager: malformed pool %q: %w", p, err)
		}
		pools = append(pools, domain.Tokens(amount))
	}

	return WagerResult{
		Balance:     domain.Tokens(balance),
		Pools:       pools,
		Total:       domain.Tokens(total),
		BettorCount: bettorCount,
	}, nil
}

// toStringSlice converts a Lua script's flat-array reply into []string.
// Every element is a Redis bulk string by construction (the scripts
// tostring() everything before returning), so a non-string element
// indicates a script bug, not a runtime condition to recover from.
func toStringSlice(res interface{}) ([]string, error) {
	arr, ok := res.([]interface{})
	if !ok {
		return nil, fmt.Errorf("redisstore: unexpected script reply type %T", res)
	}
	out := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("redisstore: unexpected script reply element type %T at index %d", v, i)
		}
		out[i] = s
	}
	return out, nil
}

// Preflight is the result of WagerPreflight's one pipelined round trip:
// the rate-limit decision, the room's current round (empty when it has
// none), and its player count. Fetching all three earlier than
// wager.Service.Place used to does not change which rejection wins —
// only the order Place examines them does.
type Preflight struct {
	Decision Decision
	RoundID  string // "" when the room has no current round
	Players  int
}

// runWagerPreflightPipeline queues and executes the three-command
// pipeline, returning each command unread — the caller decides how to
// treat a NOSCRIPT on the rate-limit leg before decoding anything.
func (s *Store) runWagerPreflightPipeline(ctx context.Context, scope, userID, roomID string, limit int, window time.Duration) (rlCmd *redis.Cmd, roundCmd *redis.StringCmd, playersCmd *redis.IntCmd) {
	member := uuid.NewString()

	pipe := s.client.Pipeline()
	rlCmd = rateLimitScript.EvalSha(ctx, pipe,
		[]string{RateLimitKey(scope, userID)},
		strconv.FormatInt(window.Milliseconds(), 10),
		strconv.Itoa(limit),
		member,
	)
	roundCmd = pipe.Get(ctx, RoomRoundKey(roomID))
	playersCmd = pipe.HLen(ctx, RoomWalletsKey(roomID))

	// Exec's own error is not decisive: it is non-nil whenever any
	// queued command errored, including the expected redis.Nil from Get
	// on a room with no round. Each leg's own result is read by the
	// caller instead.
	_, _ = pipe.Exec(ctx)

	return rlCmd, roundCmd, playersCmd
}

// WagerPreflight fetches the rate-limit decision, the room's current
// round, and the player count in one pipelined round trip — the three
// pre-write reads on wager.Service.Place's happy path that are
// independent of each other and of place_wager.lua's write. A room with
// no current round yields RoundID == "" and a nil error: the pipeline's
// redis.Nil on that leg is an expected state, not a failure. error is
// returned only for a genuine infrastructure or decode failure.
func (s *Store) WagerPreflight(ctx context.Context, scope, userID, roomID string, limit int, window time.Duration) (Preflight, error) {
	rlCmd, roundCmd, playersCmd := s.runWagerPreflightPipeline(ctx, scope, userID, roomID, limit, window)

	// A pipelined EVALSHA cannot fall back to EVAL the way Script.Run
	// does outside a pipeline (see the trap this method's package
	// comment references) — nothing has executed at queue time, so
	// Script.Run's own NOSCRIPT check never fires. Reload the script and
	// re-run the whole pipeline exactly once; a second NOSCRIPT is
	// returned as an error rather than retried, since a loop here would
	// hide a genuine fault instead of just closing the restart/flush
	// race this exists for.
	if redis.HasErrorPrefix(rlCmd.Err(), "NOSCRIPT") {
		if _, err := rateLimitScript.Load(ctx, s.client).Result(); err != nil {
			return Preflight{}, fmt.Errorf("redisstore: wager preflight %s:%s: reload rate_limit.lua: %w", scope, userID, err)
		}
		rlCmd, roundCmd, playersCmd = s.runWagerPreflightPipeline(ctx, scope, userID, roomID, limit, window)
	}

	if err := rlCmd.Err(); err != nil {
		return Preflight{}, fmt.Errorf("redisstore: wager preflight %s:%s: %w", scope, userID, err)
	}
	decision, err := decodeRateLimitReply(rlCmd.Val(), scope, userID)
	if err != nil {
		return Preflight{}, err
	}

	roundID := roundCmd.Val()
	if err := roundCmd.Err(); err != nil {
		if !errors.Is(err, redis.Nil) {
			return Preflight{}, fmt.Errorf("redisstore: wager preflight: current round for room %s: %w", roomID, err)
		}
		roundID = ""
	}

	n, err := playersCmd.Result()
	if err != nil {
		return Preflight{}, fmt.Errorf("redisstore: wager preflight: player count for room %s: %w", roomID, err)
	}
	players := int(n) - 1
	if players < 0 {
		players = 0
	}

	return Preflight{Decision: decision, RoundID: roundID, Players: players}, nil
}
