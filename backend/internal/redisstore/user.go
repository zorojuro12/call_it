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

var claimUniqueScript = redis.NewScript(lua.ClaimUnique)

// User is the account hash's typed projection.
type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string
	Balance      domain.Tokens
	CreatedAt    time.Time
}

// CreateUser claims the account's email as a unique index and creates
// its hash atomically, via claim_unique.lua — the same script room
// creation uses for its code index, since both are "claim a unique
// secondary index and create the entity it points at."
func (s *Store) CreateUser(ctx context.Context, u User) error {
	createdAtMs := time.Now().UnixMilli()

	res, err := claimUniqueScript.Run(ctx, s.client,
		[]string{EmailKey(u.Email), UserKey(u.ID)},
		u.ID,
		"email", u.Email,
		"display_name", u.DisplayName,
		"password_hash", u.PasswordHash,
		"balance", strconv.FormatInt(int64(u.Balance), 10),
		"created_at", strconv.FormatInt(createdAtMs, 10),
	).Result()
	if err != nil {
		return fmt.Errorf("redisstore: create user %s: %w", u.ID, err)
	}

	reply, err := toStringSlice(res)
	if err != nil {
		return fmt.Errorf("redisstore: create user %s: %w", u.ID, err)
	}
	if len(reply) == 0 {
		return fmt.Errorf("redisstore: create user %s: empty reply", u.ID)
	}

	switch reply[0] {
	case "OK":
		return nil
	case "TAKEN":
		return fmt.Errorf("redisstore: create user: email %s: %w", u.Email, ErrAlreadyExists)
	default:
		return fmt.Errorf("redisstore: create user %s: unrecognized status %q", u.ID, reply[0])
	}
}

// User reads an account's hash back into its typed projection.
func (s *Store) User(ctx context.Context, userID string) (User, error) {
	fields, err := s.client.HGetAll(ctx, UserKey(userID)).Result()
	if err != nil {
		return User{}, fmt.Errorf("redisstore: get user %s: %w", userID, err)
	}
	if len(fields) == 0 {
		return User{}, fmt.Errorf("redisstore: user %s: %w", userID, ErrNotFound)
	}

	balance, err := strconv.ParseInt(fields["balance"], 10, 64)
	if err != nil {
		return User{}, fmt.Errorf("redisstore: user %s: malformed balance %q: %w", userID, fields["balance"], err)
	}
	createdAtMs, err := strconv.ParseInt(fields["created_at"], 10, 64)
	if err != nil {
		return User{}, fmt.Errorf("redisstore: user %s: malformed created_at %q: %w", userID, fields["created_at"], err)
	}

	return User{
		ID:           userID,
		Email:        fields["email"],
		DisplayName:  fields["display_name"],
		PasswordHash: fields["password_hash"],
		Balance:      domain.Tokens(balance),
		CreatedAt:    time.UnixMilli(createdAtMs),
	}, nil
}

// UserByEmail resolves the email:{normalizedEmail} index, then delegates
// to User.
func (s *Store) UserByEmail(ctx context.Context, normalizedEmail string) (User, error) {
	userID, err := s.client.Get(ctx, EmailKey(normalizedEmail)).Result()
	if err == redis.Nil {
		return User{}, fmt.Errorf("redisstore: email %s: %w", normalizedEmail, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("redisstore: get email %s: %w", normalizedEmail, err)
	}
	return s.User(ctx, userID)
}
