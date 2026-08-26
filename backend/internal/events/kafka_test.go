package events

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// testBrokers mirrors the project's fail-rather-than-skip convention:
// these tests need a real broker, and a suite whose purpose is proving
// events reach Kafka must not report PASS while executing nothing.
var testBrokers []string

func TestMain(m *testing.M) {
	addr := os.Getenv("KAFKA_BROKERS")
	if addr == "" {
		addr = "localhost:9092"
	}
	testBrokers = strings.Split(addr, ",")

	conn, err := kafka.DialContext(context.Background(), "tcp", testBrokers[0])
	if err != nil {
		log.Fatalf("events: cannot reach Kafka at %s: %v — run `make up-full` and retry", testBrokers[0], err)
	}
	if err := conn.Close(); err != nil {
		log.Fatalf("events: closing Kafka dial probe failed: %v", err)
	}

	os.Exit(m.Run())
}

// topicPartitionCount reads cluster metadata and returns how many
// partitions the given topic currently has.
func topicPartitionCount(t *testing.T, ctx context.Context, topic string) int {
	t.Helper()

	conn, err := kafka.DialContext(ctx, "tcp", testBrokers[0])
	if err != nil {
		t.Fatalf("dialing broker: %v", err)
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions(topic)
	if err != nil {
		t.Fatalf("ReadPartitions(%q): %v", topic, err)
	}
	return len(partitions)
}

func TestEnsureTopicsCreatesPartitions(t *testing.T) {
	p := NewKafkaProducer(testBrokers)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := p.EnsureTopics(ctx, Partitions); err != nil {
		t.Fatalf("EnsureTopics() first call = %v, want nil", err)
	}
	if err := p.EnsureTopics(ctx, Partitions); err != nil {
		t.Fatalf("EnsureTopics() second call = %v, want nil (TOPIC_ALREADY_EXISTS must not be an error)", err)
	}

	for _, topic := range []string{TopicWagersPlaced, TopicRoundsSettled} {
		if got := topicPartitionCount(t, ctx, topic); got != Partitions {
			t.Errorf("topic %q has %d partitions, want %d", topic, got, Partitions)
		}
	}
}
