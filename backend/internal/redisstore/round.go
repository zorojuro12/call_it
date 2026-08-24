package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	lua "github.com/zorojuro12/call_it/backend/scripts/lua"
)

var lockRoundScript = redis.NewScript(lua.LockRound)

// Round is the round hash's typed projection. ResolvedOutcome is -1
// when the round has not resolved — a refunded round never gets one
// either.
type Round struct {
	ID              string
	RoomID          string
	Status          domain.RoundStatus
	LockAtMS        int64
	OutcomeCount    int
	ResolvedOutcome int
}

// CreateRound validates the outcome count, then writes the round hash
// and pre-zeroes every pool field plus the total in one transaction, so
// Pools never has to distinguish "no pool" from "zero pool".
func (s *Store) CreateRound(ctx context.Context, roundID, roomID string, outcomeCount int, lockAt time.Time) error {
	if err := domain.ValidateOutcomeCount(outcomeCount); err != nil {
		return err
	}

	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, RoundKey(roundID),
			"room_id", roomID,
			"status", string(domain.RoundOpen),
			"outcome_count", strconv.Itoa(outcomeCount),
			"lock_at_ms", strconv.FormatInt(lockAt.UnixMilli(), 10),
		)

		poolFields := make([]interface{}, 0, (outcomeCount+1)*2)
		for i := 0; i < outcomeCount; i++ {
			poolFields = append(poolFields, strconv.Itoa(i), "0")
		}
		poolFields = append(poolFields, PoolTotalField, "0")
		pipe.HSet(ctx, RoundPoolsKey(roundID), poolFields...)

		return nil
	})
	if err != nil {
		return fmt.Errorf("redisstore: create round %s: %w", roundID, err)
	}

	return nil
}

// Round reads a round's hash back into its typed projection. The stored
// status string maps directly onto domain.RoundStatus — they are the
// same strings by construction (internal/domain's round.go doc comment),
// so no translation table is needed.
func (s *Store) Round(ctx context.Context, roundID string) (Round, error) {
	fields, err := s.client.HGetAll(ctx, RoundKey(roundID)).Result()
	if err != nil {
		return Round{}, fmt.Errorf("redisstore: get round %s: %w", roundID, err)
	}
	if len(fields) == 0 {
		return Round{}, fmt.Errorf("redisstore: round %s: %w", roundID, ErrNotFound)
	}

	outcomeCount, err := strconv.Atoi(fields["outcome_count"])
	if err != nil {
		return Round{}, fmt.Errorf("redisstore: round %s: malformed outcome_count %q: %w", roundID, fields["outcome_count"], err)
	}
	lockAtMS, err := strconv.ParseInt(fields["lock_at_ms"], 10, 64)
	if err != nil {
		return Round{}, fmt.Errorf("redisstore: round %s: malformed lock_at_ms %q: %w", roundID, fields["lock_at_ms"], err)
	}

	resolvedOutcome := -1
	if v, ok := fields["resolved_outcome"]; ok {
		resolvedOutcome, err = strconv.Atoi(v)
		if err != nil {
			return Round{}, fmt.Errorf("redisstore: round %s: malformed resolved_outcome %q: %w", roundID, v, err)
		}
	}

	return Round{
		ID:              roundID,
		RoomID:          fields["room_id"],
		Status:          domain.RoundStatus(fields["status"]),
		LockAtMS:        lockAtMS,
		OutcomeCount:    outcomeCount,
		ResolvedOutcome: resolvedOutcome,
	}, nil
}

// Pools reads a round's outcome pools in index order, plus the total.
func (s *Store) Pools(ctx context.Context, roundID string) ([]domain.Tokens, domain.Tokens, error) {
	fields, err := s.client.HGetAll(ctx, RoundPoolsKey(roundID)).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("redisstore: get pools for round %s: %w", roundID, err)
	}
	if len(fields) == 0 {
		return nil, 0, fmt.Errorf("redisstore: pools for round %s: %w", roundID, ErrNotFound)
	}

	total, err := strconv.ParseInt(fields[PoolTotalField], 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("redisstore: pools for round %s: malformed total %q: %w", roundID, fields[PoolTotalField], err)
	}

	pools := make([]domain.Tokens, 0, len(fields)-1)
	for i := 0; ; i++ {
		v, ok := fields[strconv.Itoa(i)]
		if !ok {
			break
		}
		amount, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("redisstore: pools for round %s: malformed pool %d %q: %w", roundID, i, v, err)
		}
		pools = append(pools, domain.Tokens(amount))
	}

	return pools, domain.Tokens(total), nil
}

// LockRound transitions a round from open to locked. Locking an
// already-locked round is a no-op — the timer that fires this may
// retry, and a second lock is the state the caller wanted. Locking a
// resolved or refunded round returns ErrRoundTerminal, since relocking a
// resolved round would make it settleable twice.
func (s *Store) LockRound(ctx context.Context, roundID string) error {
	res, err := lockRoundScript.Run(ctx, s.client, []string{RoundKey(roundID)}).Result()
	if err != nil {
		return fmt.Errorf("redisstore: lock round %s: %w", roundID, err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return fmt.Errorf("redisstore: lock round %s: %w", roundID, err)
	}
	if len(reply) == 0 {
		return fmt.Errorf("redisstore: lock round %s: empty reply", roundID)
	}

	switch reply[0] {
	case "OK", "ALREADY_LOCKED":
		return nil
	case "ROUND_TERMINAL":
		return ErrRoundTerminal
	default:
		return fmt.Errorf("redisstore: lock round %s: unrecognized status %q", roundID, reply[0])
	}
}
