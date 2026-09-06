package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// Room is the room hash's typed projection.
type Room struct {
	ID        string
	HostID    string
	BuyIn     domain.Tokens
	Status    string
	CreatedAt time.Time
}

// CreateRoom validates the buy-in, then claims the room code as a
// unique index and creates the room hash atomically via
// claim_unique.lua. A colliding code returns ErrAlreadyExists instead
// of silently repointing the existing room's code — the old
// unconditional SET would send every subsequent joiner to the wrong
// room while reporting success.
func (s *Store) CreateRoom(ctx context.Context, roomID, code, hostID string, buyIn domain.Tokens) error {
	if err := domain.ValidateBuyIn(buyIn); err != nil {
		return err
	}

	createdAtMs := time.Now().UnixMilli()

	res, err := claimUniqueScript.Run(ctx, s.client,
		[]string{RoomCodeKey(code), RoomKey(roomID)},
		roomID,
		"host_id", hostID,
		"buy_in", strconv.FormatInt(int64(buyIn), 10),
		"status", "open",
		"created_at", strconv.FormatInt(createdAtMs, 10),
	).Result()
	if err != nil {
		return fmt.Errorf("redisstore: create room %s: %w", roomID, err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return fmt.Errorf("redisstore: create room %s: %w", roomID, err)
	}
	if len(reply) == 0 {
		return fmt.Errorf("redisstore: create room %s: empty reply", roomID)
	}

	switch reply[0] {
	case "OK":
		return nil
	case "TAKEN":
		return fmt.Errorf("redisstore: create room: code %s: %w", code, ErrAlreadyExists)
	default:
		return fmt.Errorf("redisstore: create room %s: unrecognized status %q", roomID, reply[0])
	}
}

// RoomByCode resolves a room's short code to its room ID.
func (s *Store) RoomByCode(ctx context.Context, code string) (string, error) {
	roomID, err := s.client.Get(ctx, RoomCodeKey(code)).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("redisstore: code %s: %w", code, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("redisstore: get code %s: %w", code, err)
	}
	return roomID, nil
}

// Room reads a room's hash back into its typed projection.
func (s *Store) Room(ctx context.Context, roomID string) (Room, error) {
	fields, err := s.client.HGetAll(ctx, RoomKey(roomID)).Result()
	if err != nil {
		return Room{}, fmt.Errorf("redisstore: get room %s: %w", roomID, err)
	}
	if len(fields) == 0 {
		return Room{}, fmt.Errorf("redisstore: room %s: %w", roomID, ErrNotFound)
	}

	buyIn, err := strconv.ParseInt(fields["buy_in"], 10, 64)
	if err != nil {
		return Room{}, fmt.Errorf("redisstore: room %s: malformed buy_in %q: %w", roomID, fields["buy_in"], err)
	}
	createdAtMs, err := strconv.ParseInt(fields["created_at"], 10, 64)
	if err != nil {
		return Room{}, fmt.Errorf("redisstore: room %s: malformed created_at %q: %w", roomID, fields["created_at"], err)
	}

	return Room{
		ID:        roomID,
		HostID:    fields["host_id"],
		BuyIn:     domain.Tokens(buyIn),
		Status:    fields["status"],
		CreatedAt: time.UnixMilli(createdAtMs),
	}, nil
}

// JoinRoom materializes a session wallet for userID at the given
// balance on a first join. On a rejoin — a page refresh, a dropped
// connection — the wallet is left exactly as it stands: HSETNX means an
// existing field is never overwritten, so a participant who is losing
// cannot reload the page to reset their wallet back to the full buy-in.
// The caller decides the balance for a first join — domain.
// GuestSessionBalance or domain.AccountSessionBalance — this layer does
// not re-derive it. The returned effective balance is the newly seeded
// one on a first join, or the surviving one on a rejoin.
func (s *Store) JoinRoom(ctx context.Context, roomID, userID string, balance domain.Tokens) (effective domain.Tokens, err error) {
	if balance <= 0 {
		return 0, fmt.Errorf("%w: balance %d must be positive", domain.ErrInvalidStake, balance)
	}

	// The opening stake is written in the same transaction as the
	// wallet — Amendment D3 — so a wallet can never exist without its
	// opening stake. HSetNX on both means a rejoin leaves each exactly
	// as it stands, same as the wallet's own rejoin behavior.
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSetNX(ctx, RoomWalletsKey(roomID), userID, strconv.FormatInt(int64(balance), 10))
		pipe.HSetNX(ctx, RoomOpeningKey(roomID), userID, strconv.FormatInt(int64(balance), 10))
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("redisstore: join room %s as %s: %w", roomID, userID, err)
	}

	v, err := s.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
	if err != nil {
		return 0, fmt.Errorf("redisstore: join room %s as %s: read back balance: %w", roomID, userID, err)
	}
	eff, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("redisstore: join room %s as %s: malformed balance %q: %w", roomID, userID, v, err)
	}

	return domain.Tokens(eff), nil
}

// ClearSession claims and clears one user's session state in a room,
// deleting their fields from both the wallets hash and the
// opening-stake hash in one pipeline. claimed reports whether the
// opening-stake field existed before this call — that field, not the
// wallet, is the claim token: settle_round.lua can resurrect a deleted
// wallet field via HINCRBY when it credits a winner, so only the
// opening stake (written once, by JoinRoom, and never rewritten) can
// tell a caller "this call was the one that ended the session" from
// "someone already ended it." Exactly one concurrent caller observes
// claimed == true for a given session.
func (s *Store) ClearSession(ctx context.Context, roomID, userID string) (claimed bool, err error) {
	cmds, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HDel(ctx, RoomOpeningKey(roomID), userID)
		pipe.HDel(ctx, RoomWalletsKey(roomID), userID)
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("redisstore: clear session for %s in room %s: %w", userID, roomID, err)
	}

	openingDeleted, err := cmds[0].(*redis.IntCmd).Result()
	if err != nil {
		return false, fmt.Errorf("redisstore: clear session for %s in room %s: %w", userID, roomID, err)
	}

	return openingDeleted == 1, nil
}

// Balance reads a user's session balance in a room.
func (s *Store) Balance(ctx context.Context, roomID, userID string) (domain.Tokens, error) {
	v, err := s.client.HGet(ctx, RoomWalletsKey(roomID), userID).Result()
	if err == redis.Nil {
		return 0, fmt.Errorf("redisstore: balance for %s in room %s: %w", userID, roomID, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("redisstore: get balance for %s in room %s: %w", userID, roomID, err)
	}
	balance, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("redisstore: balance for %s in room %s: malformed value %q: %w", userID, roomID, v, err)
	}
	return domain.Tokens(balance), nil
}

// OpeningStake reads a user's opening session stake in a room — the
// effective balance granted at join, which stays fixed through the
// session even as the wallet moves on every wager (Amendment D3).
func (s *Store) OpeningStake(ctx context.Context, roomID, userID string) (domain.Tokens, error) {
	v, err := s.client.HGet(ctx, RoomOpeningKey(roomID), userID).Result()
	if err == redis.Nil {
		return 0, fmt.Errorf("redisstore: opening stake for %s in room %s: %w", userID, roomID, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("redisstore: get opening stake for %s in room %s: %w", userID, roomID, err)
	}
	balance, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("redisstore: opening stake for %s in room %s: malformed value %q: %w", userID, roomID, v, err)
	}
	return domain.Tokens(balance), nil
}

// PlayerCount returns the number of players who can wager in the room —
// every wallet field except the host's, since the host cannot wager and
// the "N/M players" progress denominator (spec §4) counts only those who
// can.
func (s *Store) PlayerCount(ctx context.Context, roomID string) (int, error) {
	n, err := s.client.HLen(ctx, RoomWalletsKey(roomID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redisstore: player count for room %s: %w", roomID, err)
	}
	count := int(n) - 1
	if count < 0 {
		return 0, nil
	}
	return count, nil
}
