// Package redisstore is the only place in the codebase permitted to
// construct a Redis key. The schema it encodes is the parent plan
// (docs/plans/2026-08-21-implementation-plan.md) §4 table, plus the
// round:{roundID}:bettors set added by Phase 2's Amendment A2.
package redisstore

import (
	"fmt"
	"strconv"
	"strings"
)

// OutboxStream is the name of the single Redis Stream that every
// balance-mutating script XADDs into, atomically with its mutation.
const OutboxStream = "wager-outbox"

// PoolTotalField is the field in a round's pools hash holding the sum of
// every outcome's pool.
const PoolTotalField = "total"

func RoomKey(roomID string) string {
	return "room:" + roomID
}

func RoomWalletsKey(roomID string) string {
	return "room:" + roomID + ":wallets"
}

func RoomCodeKey(code string) string {
	return "code:" + code
}

func RoundKey(roundID string) string {
	return "round:" + roundID
}

// RoomRoundKey indexes a room's current (non-terminal) round, so finding
// it never requires scanning every round key (Amendment D2).
func RoomRoundKey(roomID string) string {
	return "room:" + roomID + ":round"
}

// RoomOpeningKey holds each player's opening session stake — the
// effective balance granted at join, which never moves after (Amendment
// D3). Needed to compute a session's net delta at EndSession, since the
// wallet itself moves on every wager.
func RoomOpeningKey(roomID string) string {
	return "room:" + roomID + ":opening"
}

func RoundPoolsKey(roundID string) string {
	return "round:" + roundID + ":pools"
}

func RoundWagersKey(roundID string) string {
	return "round:" + roundID + ":wagers"
}

func RoundBettorsKey(roundID string) string {
	return "round:" + roundID + ":bettors"
}

func IdemKey(key string) string {
	return "idem:" + key
}

func UserKey(userID string) string {
	return "user:" + userID
}

func EmailKey(normalizedEmail string) string {
	return "email:" + normalizedEmail
}

func RateLimitKey(scope, id string) string {
	return "ratelimit:" + scope + ":" + id
}

// WagerField builds a field name for the round:{roundID}:wagers hash.
func WagerField(userID string, outcome int) string {
	return userID + ":" + strconv.Itoa(outcome)
}

// ParseWagerField recovers the user ID and outcome index from a
// round:{roundID}:wagers field name. It splits on the last colon, so a
// user ID containing a colon cannot corrupt the outcome index.
func ParseWagerField(field string) (userID string, outcome int, err error) {
	idx := strings.LastIndex(field, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("redisstore: malformed wager field %q: no colon", field)
	}

	userID = field[:idx]
	outcomeStr := field[idx+1:]

	outcome, err = strconv.Atoi(outcomeStr)
	if err != nil {
		return "", 0, fmt.Errorf("redisstore: malformed wager field %q: invalid outcome index: %w", field, err)
	}

	return userID, outcome, nil
}
