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

// CreateRoom validates the buy-in, then writes the room hash and its
// code lookup in a single transaction, so a room can never exist
// without its code mapping.
func (s *Store) CreateRoom(ctx context.Context, roomID, code, hostID string, buyIn domain.Tokens) error {
	if err := domain.ValidateBuyIn(buyIn); err != nil {
		return err
	}

	createdAtMs := time.Now().UnixMilli()

	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, RoomKey(roomID),
			"host_id", hostID,
			"buy_in", strconv.FormatInt(int64(buyIn), 10),
			"status", "open",
			"created_at", strconv.FormatInt(createdAtMs, 10),
		)
		pipe.Set(ctx, RoomCodeKey(code), roomID, 0)
		return nil
	})
	if err != nil {
		return fmt.Errorf("redisstore: create room %s: %w", roomID, err)
	}

	return nil
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
// balance. The caller decides the balance — domain.GuestSessionBalance
// or domain.AccountSessionBalance — this layer does not re-derive it.
func (s *Store) JoinRoom(ctx context.Context, roomID, userID string, balance domain.Tokens) error {
	if balance <= 0 {
		return fmt.Errorf("%w: balance %d must be positive", domain.ErrInvalidStake, balance)
	}

	if err := s.client.HSet(ctx, RoomWalletsKey(roomID), userID, strconv.FormatInt(int64(balance), 10)).Err(); err != nil {
		return fmt.Errorf("redisstore: join room %s as %s: %w", roomID, userID, err)
	}
	return nil
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
