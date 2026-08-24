package redisstore

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Store wraps a Redis client and the name of the outbox stream this
// instance writes to. The stream name is a field, not the OutboxStream
// constant directly, because wager-outbox is the one key in the schema
// not namespaced by a room or round ID — tests need distinct streams to
// stay independent, while production uses the shared default.
type Store struct {
	client       *redis.Client
	outboxStream string
}

// New constructs a Store and fails fast if Redis is unreachable, matching
// the fail-fast posture internal/config already takes.
func New(addr string, db int) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   db,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redisstore: connect to %s db %d: %w", addr, db, err)
	}

	return &Store{
		client:       client,
		outboxStream: OutboxStream,
	}, nil
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
