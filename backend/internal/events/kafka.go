package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"
)

// Partitions is the partition count both wagers-placed and
// rounds-settled are created with (parent plan §7). Kafka cannot lower a
// topic's partition count later, and relying on broker auto-creation
// (docker-compose.yml sets KAFKA_AUTO_CREATE_TOPICS_ENABLE) would yield
// the broker default of 1 — Amendment E3 is why EnsureTopics creates
// both topics explicitly rather than letting the first Produce do it.
const Partitions = 6

// Producer sends events to Kafka. Satisfies relay.Producer structurally
// — internal/events does not import internal/relay, so no cycle exists.
type KafkaProducer struct {
	brokers []string
	writer  *kafka.Writer
}

// NewKafkaProducer builds a producer over the given broker addresses. It
// performs no I/O — call EnsureTopics before the first Produce.
func NewKafkaProducer(brokers []string) *KafkaProducer {
	return &KafkaProducer{
		brokers: brokers,
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
		},
	}
}

// EnsureTopics idempotently creates wagers-placed and rounds-settled
// with the given partition count. A TOPIC_ALREADY_EXISTS response is
// treated as success, not an error — a second call (e.g. a second relay
// process starting up) must be a no-op.
func (p *KafkaProducer) EnsureTopics(ctx context.Context, partitions int) error {
	conn, err := kafka.DialContext(ctx, "tcp", p.brokers[0])
	if err != nil {
		return fmt.Errorf("events: dialing broker %s: %w", p.brokers[0], err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("events: finding controller broker: %w", err)
	}
	controllerConn, err := kafka.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("events: dialing controller broker: %w", err)
	}
	defer controllerConn.Close()

	topics := []kafka.TopicConfig{
		{Topic: TopicWagersPlaced, NumPartitions: partitions, ReplicationFactor: 1},
		{Topic: TopicRoundsSettled, NumPartitions: partitions, ReplicationFactor: 1},
	}

	// Conn.CreateTopics already treats a per-topic TopicAlreadyExists
	// response as a no-op rather than an error, which is what makes a
	// second EnsureTopics call (e.g. a second relay process starting up)
	// idempotent.
	if err := controllerConn.CreateTopics(topics...); err != nil {
		return fmt.Errorf("events: creating topics: %w", err)
	}

	return nil
}

// Produce writes each event as one Kafka message, keyed by its
// PartitionKey so events sharing a key always land on the same
// partition — the real per-room ordering guarantee, not a nominal one.
// RequiredAcks: RequireAll on the writer means a produce does not return
// successfully until the broker has durably accepted the write — the
// relay must not ack a Redis entry against a write the broker can still
// lose, which would reopen the crash window the outbox exists to close.
func (p *KafkaProducer) Produce(ctx context.Context, evs []Event) error {
	msgs := make([]kafka.Message, len(evs))
	for i, ev := range evs {
		v, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("events: marshaling event for topic %s: %w", ev.Topic(), err)
		}
		msgs[i] = kafka.Message{
			Topic: ev.Topic(),
			Key:   []byte(ev.PartitionKey()),
			Value: v,
		}
	}

	if err := p.writer.WriteMessages(ctx, msgs...); err != nil {
		return fmt.Errorf("events: writing %d messages to Kafka: %w", len(msgs), err)
	}
	return nil
}

// Close releases the underlying writer's connections.
func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
