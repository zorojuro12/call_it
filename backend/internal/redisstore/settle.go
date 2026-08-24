package redisstore

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// ReadStakes reads a round's wagers hash into domain.Stake, sorted by
// (UserID, Outcome).
//
// The sort is required, not cosmetic. HGETALL returns fields in
// unspecified order, and domain.Settle emits Settlement.Results in the
// order players first appear in its input. Without sorting, settling
// the same round twice could produce the reveal rows in different
// orders. This narrows Phase 1's documented "order they first staked"
// to "ascending by user ID" for anything sourced from Redis;
// domain.Settle itself is unchanged.
func (s *Store) ReadStakes(ctx context.Context, roundID string) ([]domain.Stake, error) {
	fields, err := s.client.HGetAll(ctx, RoundWagersKey(roundID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redisstore: read stakes for round %s: %w", roundID, err)
	}

	stakes := make([]domain.Stake, 0, len(fields))
	for field, v := range fields {
		userID, outcome, err := ParseWagerField(field)
		if err != nil {
			return nil, fmt.Errorf("redisstore: read stakes for round %s: %w", roundID, err)
		}
		amount, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redisstore: read stakes for round %s: malformed amount %q for field %q: %w", roundID, v, field, err)
		}
		stakes = append(stakes, domain.Stake{UserID: userID, Outcome: outcome, Amount: domain.Tokens(amount)})
	}

	sort.Slice(stakes, func(i, j int) bool {
		if stakes[i].UserID != stakes[j].UserID {
			return stakes[i].UserID < stakes[j].UserID
		}
		return stakes[i].Outcome < stakes[j].Outcome
	})

	return stakes, nil
}
