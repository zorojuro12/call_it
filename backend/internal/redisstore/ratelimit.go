package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	lua "github.com/zorojuro12/call_it/backend/scripts/lua"
)

var rateLimitScript = redis.NewScript(lua.RateLimit)

// Decision is the outcome of one Allow call.
type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration // zero when Allowed
	Member     string        // the ZSET member recorded; "" when denied
	ResetAt    time.Time     // when the window's oldest hit ages out
}

// Allow checks and records one attempt against a sliding window, atomically
// via rate_limit.lua. The clock is Redis's TIME, not Go's — the same rule
// that makes wager lockout immune to skew across API instances applies to
// a quota that must count the same way from every instance.
func (s *Store) Allow(ctx context.Context, scope, id string, limit int, window time.Duration) (Decision, error) {
	member := uuid.NewString()

	res, err := rateLimitScript.Run(ctx, s.client,
		[]string{RateLimitKey(scope, id)},
		strconv.FormatInt(window.Milliseconds(), 10),
		strconv.Itoa(limit),
		member,
	).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: %w", scope, id, err)
	}

	return decodeRateLimitReply(res, scope, id)
}

// decodeRateLimitReply decodes rate_limit.lua's reply into a Decision.
// Shared by Allow (a standalone EVALSHA) and WagerPreflight (the same
// script queued inside a pipeline) — one limiter, one decoder, never two
// copies of this parsing.
func decodeRateLimitReply(res interface{}, scope, id string) (Decision, error) {
	reply, err := toStringSlice(res)
	if err != nil {
		return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: %w", scope, id, err)
	}
	if len(reply) < 4 {
		return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: reply has %d elements, want at least 4", scope, id, len(reply))
	}

	switch reply[0] {
	case "ALLOWED":
		remaining, err := strconv.Atoi(reply[1])
		if err != nil {
			return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: malformed remaining %q: %w", scope, id, reply[1], err)
		}
		resetAtMs, err := strconv.ParseInt(reply[3], 10, 64)
		if err != nil {
			return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: malformed resetAt %q: %w", scope, id, reply[3], err)
		}
		return Decision{
			Allowed:   true,
			Remaining: remaining,
			Member:    reply[2],
			ResetAt:   time.UnixMilli(resetAtMs),
		}, nil
	case "DENIED":
		if len(reply) < 5 {
			return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: DENIED reply has %d elements, want 5", scope, id, len(reply))
		}
		retryAfterMs, err := strconv.ParseInt(reply[3], 10, 64)
		if err != nil {
			return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: malformed retryAfter %q: %w", scope, id, reply[3], err)
		}
		resetAtMs, err := strconv.ParseInt(reply[4], 10, 64)
		if err != nil {
			return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: malformed resetAt %q: %w", scope, id, reply[4], err)
		}
		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
			ResetAt:    time.UnixMilli(resetAtMs),
		}, nil
	default:
		return Decision{}, fmt.Errorf("redisstore: rate limit %s:%s: unrecognized status %q", scope, id, reply[0])
	}
}

// Revoke hands a recorded slot back — used when a request that consumed
// quota turned out to credit nothing, so a doomed request doesn't
// permanently cost the caller a slot. A single ZREM is already atomic,
// so no script is needed. Revoking a member that was never recorded (or
// already evicted) is a no-op, not an error.
func (s *Store) Revoke(ctx context.Context, scope, id, member string) error {
	if err := s.client.ZRem(ctx, RateLimitKey(scope, id), member).Err(); err != nil {
		return fmt.Errorf("redisstore: revoke %s:%s member %s: %w", scope, id, member, err)
	}
	return nil
}
