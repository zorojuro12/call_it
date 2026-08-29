package events

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

func TestKafkaConsumerReadsMultipleTopicsUnderOneGroup(t *testing.T) {
	p := NewKafkaProducer(testBrokers)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ensure topics exist
	if err := p.EnsureTopics(ctx, Partitions); err != nil {
		t.Fatalf("EnsureTopics() = %v, want nil", err)
	}

	// RoomID, RoundID, and every UserID must be real UUIDs, not
	// testTopic()'s "kind-testname-n" strings: internal/ledger's worker
	// consumes this same shared topic from FirstOffset and writes these
	// fields into PostgreSQL uuid columns (Task 6 discovery — the local
	// Kafka topic retains every run's messages, so a non-UUID value
	// produced here breaks that consumer's insert with SQLSTATE 22P02).
	roomID := uuid.NewString()
	roundID := uuid.NewString()
	userID := uuid.NewString()
	wager := WagerPlaced{
		RoomID: roomID, RoundID: roundID, UserID: userID,
		IdempotencyKey: testTopic(t, "idem-w"), Outcome: 0, Amount: 100, Balance: 900,
	}
	settled := RoundSettled{
		RoomID: roomID, RoundID: roundID, IdempotencyKey: testTopic(t, "idem-s"),
		WinningOutcome: 0, Total: 100, Dust: 0,
		Payouts: []Payout{{UserID: userID, Amount: 100}}, Refunded: false,
	}

	if err := p.Produce(ctx, []Event{wager, settled}); err != nil {
		t.Fatalf("Produce() = %v, want nil", err)
	}

	// Create consumer with unique group reading from beginning
	group := testTopic(t, "ledger-consumer")
	consumer := NewKafkaConsumer(testBrokers, group, []string{TopicWagersPlaced, TopicRoundsSettled}, true)
	defer consumer.Close()

	// Fetch enough messages to get both topics (with timeout to give Kafka time to deliver)
	msgs := make([]kafka.Message, 0, 10)
	topicsSeen := make(map[string]bool)
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		if len(topicsSeen) >= 2 {
			// We have messages from both topics
			break
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		msg, err := consumer.Fetch(fetchCtx)
		cancel()
		if err != nil {
			// Timeout is ok, just retry
			continue
		}
		// Only collect messages from our specific room
		if msg.Key != nil && string(msg.Key) == roomID {
			msgs = append(msgs, msg)
			topicsSeen[msg.Topic] = true
		}
	}

	if len(topicsSeen) != 2 {
		t.Fatalf("Fetch() saw %d topics (%v), want both wagers-placed and rounds-settled. Got %d messages", len(topicsSeen), topicsSeen, len(msgs))
	}

	// Verify messages decode
	for _, msg := range msgs {
		ev, err := DecodeMessage(msg.Topic, msg.Value)
		if err != nil {
			t.Fatalf("DecodeMessage(%s, ...) = %v, want nil", msg.Topic, err)
		}
		_ = ev // just verify it decoded
	}

	// Close without committing
	consumer.Close()

	// Small delay for offset state to stabilize
	time.Sleep(100 * time.Millisecond)

	// Open a new consumer on the same group and verify offsets were not committed
	consumer2 := NewKafkaConsumer(testBrokers, group, []string{TopicWagersPlaced, TopicRoundsSettled}, true)
	defer consumer2.Close()

	// Fetch again - should see messages again since we never committed
	msgs2 := make([]kafka.Message, 0, 10)
	topicsSeen2 := make(map[string]bool)
	deadline = time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		if len(topicsSeen2) >= 2 {
			break
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		msg, err := consumer2.Fetch(fetchCtx)
		cancel()
		if err != nil {
			continue
		}
		// Only collect messages from our specific room
		if msg.Key != nil && string(msg.Key) == roomID {
			msgs2 = append(msgs2, msg)
			topicsSeen2[msg.Topic] = true
		}
	}

	if len(topicsSeen2) != 2 {
		t.Errorf("second consumer saw %d topics (%v), want both wagers-placed and rounds-settled. Messages: %d", len(topicsSeen2), topicsSeen2, len(msgs2))
	}
}
