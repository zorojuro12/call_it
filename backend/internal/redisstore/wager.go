package redisstore

import (
	"context"
	"fmt"
	"strconv"

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
