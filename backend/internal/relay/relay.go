// Package relay reads the wager-outbox Redis Stream through a consumer
// group and produces each entry to Kafka, acking only after the produce
// is confirmed — an at-least-once bridge that never acks ahead of a
// durable write, per the outbox pattern CLAUDE.md documents.
package relay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/events"
)

// Producer sends a batch of decoded events to their destination (Kafka
// in production, a fake in tests). Declared here and satisfied
// structurally by events.KafkaProducer, so this package does not import
// internal/events' Kafka code and no import cycle exists.
type Producer interface {
	Produce(ctx context.Context, evs []events.Event) error
}

// Relay reads OutboxStream through a Redis consumer group, decodes each
// entry, and hands the batch to a Producer.
type Relay struct {
	client   *redis.Client
	stream   string
	group    string
	consumer string
	producer Producer
}

// New builds a Relay. It performs no I/O — call EnsureGroup before Once
// or Run.
func New(client *redis.Client, stream, group, consumer string, p Producer) *Relay {
	return &Relay{
		client:   client,
		stream:   stream,
		group:    group,
		consumer: consumer,
		producer: p,
	}
}

// EnsureGroup creates the consumer group if it does not already exist,
// starting from stream id "0" rather than "$" — "$" would skip every
// entry already written by a running API process, silently losing money
// movements that predate the relay's first start. Idempotent: a second
// call swallows Redis's BUSYGROUP error and returns nil.
func (r *Relay) EnsureGroup(ctx context.Context) error {
	err := r.client.XGroupCreateMkStream(ctx, r.stream, r.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("relay: creating consumer group %q on stream %q: %w", r.group, r.stream, err)
	}
	return nil
}

// Once runs one read → produce → ack cycle: XREADGROUP up to count new
// entries (blocking up to block for at least one), decode the whole
// batch, hand it to the producer in a single call, and ack every id only
// after Produce returns nil. An empty read (block timeout) returns
// (0, nil).
func (r *Relay) Once(ctx context.Context, count int64, block time.Duration) (int, error) {
	streams, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    r.group,
		Consumer: r.consumer,
		Streams:  []string{r.stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("relay: XREADGROUP on stream %q: %w", r.stream, err)
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return 0, nil
	}

	messages := streams[0].Messages
	evs := make([]events.Event, 0, len(messages))
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		fields := make(map[string]string, len(msg.Values))
		for k, v := range msg.Values {
			fields[k], _ = v.(string)
		}
		ev, err := events.Decode(fields)
		if err != nil {
			return 0, fmt.Errorf("relay: decoding entry %s: %w", msg.ID, err)
		}
		evs = append(evs, ev)
		ids = append(ids, msg.ID)
	}

	if err := r.producer.Produce(ctx, evs); err != nil {
		return 0, fmt.Errorf("relay: producing batch of %d: %w", len(evs), err)
	}

	if err := r.client.XAck(ctx, r.stream, r.group, ids...).Err(); err != nil {
		return 0, fmt.Errorf("relay: acking %d entries: %w", len(ids), err)
	}

	return len(evs), nil
}
