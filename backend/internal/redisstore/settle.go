package redisstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	lua "github.com/zorojuro12/call_it/backend/scripts/lua"
)

// wirePayout is the outbox event's JSON shape for one payout. Kept
// separate from domain.Payout so the domain type never grows wire-format
// tags (Amendment E1).
type wirePayout struct {
	UserID string `json:"user_id"`
	Amount int64  `json:"amount"`
}

// aggregateStakes sums stakes per user into one payout per account,
// since a ledger credit line is per account, not per stake — a player
// with stakes on two different outcomes of a refunded round gets one
// combined credit. The result is sorted ascending by user ID because
// ReadStakes returns stakes already sorted that way, and the same round
// refunded twice must produce identical output.
func aggregateStakes(stakes []domain.Stake) ([]domain.Payout, domain.Tokens) {
	index := make(map[string]int, len(stakes))
	payouts := make([]domain.Payout, 0, len(stakes))
	var total domain.Tokens

	for _, st := range stakes {
		i, seen := index[st.UserID]
		if !seen {
			i = len(payouts)
			index[st.UserID] = i
			payouts = append(payouts, domain.Payout{UserID: st.UserID})
		}
		payouts[i].Amount += st.Amount
		total += st.Amount
	}

	return payouts, total
}

// marshalPayouts renders payouts as the outbox wire format. An empty
// slice marshals to "[]", never "null" — the payouts field always
// carries a JSON array, even when settlement produced no payouts.
func marshalPayouts(payouts []domain.Payout) (string, error) {
	wire := make([]wirePayout, len(payouts))
	for i, p := range payouts {
		wire[i] = wirePayout{UserID: p.UserID, Amount: int64(p.Amount)}
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("redisstore: marshal payouts: %w", err)
	}
	return string(b), nil
}

var settleRoundScript = redis.NewScript(lua.SettleRound)
var refundRoundScript = redis.NewScript(lua.RefundRound)

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

// SettleRound settles a locked round: Go computes the payout via
// domain.Settle (Amendment A1 — settlement math is not duplicated in
// Lua), then settle_round.lua applies it atomically — CAS to a terminal
// status, credit every payout, emit one outbox event.
func (s *Store) SettleRound(ctx context.Context, roundID string, winningOutcome int, idempotencyKey string) (domain.Settlement, error) {
	round, err := s.Round(ctx, roundID)
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}
	// Checked here too, not just in the script: an unlocked round should
	// fail without computing a settlement it will never apply. The
	// script keeps its own check regardless, since this read is not
	// atomic with it.
	switch round.Status {
	case domain.RoundResolved, domain.RoundRefunded:
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, ErrAlreadySettled)
	case domain.RoundLocked:
		// proceed
	default:
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, ErrNotLocked)
	}

	stakes, err := s.ReadStakes(ctx, roundID)
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}

	settlement, err := domain.Settle(stakes, winningOutcome, round.OutcomeCount)
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}

	terminalStatus := "resolved"
	resolvedOutcomeArg := strconv.Itoa(winningOutcome)
	if settlement.Refunded {
		terminalStatus = "refunded"
		resolvedOutcomeArg = ""
	}

	var total domain.Tokens
	for _, st := range stakes {
		total += st.Amount
	}

	payoutsJSON, err := marshalPayouts(settlement.Payouts)
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}

	keys := []string{RoundKey(roundID), RoomWalletsKey(round.RoomID), s.outboxStream}
	argv := []interface{}{
		terminalStatus,
		resolvedOutcomeArg,
		strconv.FormatInt(int64(settlement.Dust), 10),
		idempotencyKey,
		roundID,
		round.RoomID,
		strconv.FormatInt(int64(total), 10),
		payoutsJSON,
	}
	for _, p := range settlement.Payouts {
		argv = append(argv, p.UserID, strconv.FormatInt(int64(p.Amount), 10))
	}

	res, err := settleRoundScript.Run(ctx, s.client, keys, argv...).Result()
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: %w", roundID, err)
	}
	if len(reply) == 0 {
		return domain.Settlement{}, fmt.Errorf("redisstore: settle round %s: empty reply", roundID)
	}
	if reply[0] != "OK" {
		return domain.Settlement{}, mapSettleStatus(reply)
	}

	return settlement, nil
}

// RefundRound refunds every stake on a locked round's timeout/disconnect
// path. Refunding is the identity function on stakes, but the outbox
// event still needs a per-user payout breakdown (Amendment E1), so Go
// now reads and aggregates stakes itself, symmetric with SettleRound
// (Amendment E2).
func (s *Store) RefundRound(ctx context.Context, roundID, idempotencyKey string) (domain.Tokens, error) {
	round, err := s.Round(ctx, roundID)
	if err != nil {
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, err)
	}

	// Checked here too, not just in the script: an unlocked round should
	// fail before Go reads stakes, never after. The read below is
	// deliberately placed inside this locked branch rather than before
	// the switch — reading stakes on an open round could be applied to a
	// since-locked round (place_wager.lua rejects new wagers on a locked
	// round, but a read taken before the lock predates that guarantee)
	// and would silently drop every wager placed in the gap between the
	// read and the lock. No test can catch a refactor that hoists this
	// read out of the switch, so this ordering is the guard.
	var payouts []domain.Payout
	var total domain.Tokens
	switch round.Status {
	case domain.RoundResolved, domain.RoundRefunded:
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, ErrAlreadySettled)
	case domain.RoundLocked:
		stakes, err := s.ReadStakes(ctx, roundID)
		if err != nil {
			return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, err)
		}
		payouts, total = aggregateStakes(stakes)
	default:
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, ErrNotLocked)
	}

	payoutsJSON, err := marshalPayouts(payouts)
	if err != nil {
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, err)
	}

	keys := []string{RoundKey(roundID), RoomWalletsKey(round.RoomID), s.outboxStream}
	argv := []interface{}{idempotencyKey, roundID, round.RoomID, strconv.FormatInt(int64(total), 10), payoutsJSON}
	for _, p := range payouts {
		argv = append(argv, p.UserID, strconv.FormatInt(int64(p.Amount), 10))
	}

	res, err := refundRoundScript.Run(ctx, s.client, keys, argv...).Result()
	if err != nil {
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return 0, fmt.Errorf("redisstore: refund round %s: %w", roundID, err)
	}
	if len(reply) == 0 {
		return 0, fmt.Errorf("redisstore: refund round %s: empty reply", roundID)
	}
	if reply[0] != "OK" {
		return 0, mapSettleStatus(reply)
	}

	return total, nil
}
