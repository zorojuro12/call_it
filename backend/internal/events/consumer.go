package events

import (
	"context"

	"github.com/segmentio/kafka-go"
)

// KafkaConsumer reads from Kafka topics under a consumer group without
// auto-committing offsets. This ensures the offset never advances ahead of
// the PostgreSQL ledger write, the same rule relay follows for its Redis
// XACK — a crash after committing but before persisting data loses a money
// movement permanently.
type KafkaConsumer struct {
	reader *kafka.Reader
}

// NewKafkaConsumer joins group over the given topics. startFromBeginning
// selects kafka.FirstOffset for a group with no committed offset.
func NewKafkaConsumer(brokers []string, group string, topics []string, startFromBeginning bool) *KafkaConsumer {
	startOffset := kafka.LastOffset
	if startFromBeginning {
		startOffset = kafka.FirstOffset
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        group,
		GroupTopics:    topics,
		StartOffset:    startOffset,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0, // disabled; we commit manually
	})

	return &KafkaConsumer{reader: reader}
}

// Fetch returns the next message without committing its offset.
func (c *KafkaConsumer) Fetch(ctx context.Context) (kafka.Message, error) {
	return c.reader.FetchMessage(ctx)
}

// Commit marks msgs consumed. Never call it before the durable write.
func (c *KafkaConsumer) Commit(ctx context.Context, msgs ...kafka.Message) error {
	return c.reader.CommitMessages(ctx, msgs...)
}

// Close closes the consumer.
func (c *KafkaConsumer) Close() error {
	return c.reader.Close()
}
