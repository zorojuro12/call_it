package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
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

var testTopicCounter atomic.Uint64

// testTopic returns a collision-free string (room ID, idempotency key,
// consumer group name, ...) so runs do not read each other's messages.
func testTopic(t *testing.T, kind string) string {
	t.Helper()
	n := testTopicCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", kind, t.Name(), n)
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

func TestProduceRoundTrip(t *testing.T) {
	p := NewKafkaProducer(testBrokers)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := p.EnsureTopics(ctx, Partitions); err != nil {
		t.Fatalf("EnsureTopics() = %v, want nil", err)
	}

	roomID := testTopic(t, "room")
	wager := WagerPlaced{
		RoomID: roomID, RoundID: "rd1", UserID: "u1",
		IdempotencyKey: testTopic(t, "idem-w"), Outcome: 0, Amount: 100, Balance: 900,
	}
	settled := RoundSettled{
		RoomID: roomID, RoundID: "rd1", IdempotencyKey: testTopic(t, "idem-s"),
		WinningOutcome: 0, Total: 100, Dust: 0,
		Payouts: []Payout{{UserID: "u1", Amount: 100}}, Refunded: false,
	}

	if err := p.Produce(ctx, []Event{wager, settled}); err != nil {
		t.Fatalf("Produce() = %v, want nil", err)
	}

	// GroupID (rather than a fixed Partition) reads across every
	// partition of the topic — the message's room-keyed partition is
	// unknown ahead of time, and StartOffset: FirstOffset with a fresh
	// group name ensures this test sees the whole topic from the start.
	wagerReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     testBrokers,
		Topic:       TopicWagersPlaced,
		GroupID:     testTopic(t, "group-wager"),
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
	})
	defer wagerReader.Close()

	settledReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     testBrokers,
		Topic:       TopicRoundsSettled,
		GroupID:     testTopic(t, "group-settled"),
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
	})
	defer settledReader.Close()

	wagerMsg := readUntilKey(t, ctx, wagerReader, roomID)
	settledMsg := readUntilKey(t, ctx, settledReader, roomID)

	if string(wagerMsg.Key) != roomID {
		t.Errorf("wager message key = %q, want %q", wagerMsg.Key, roomID)
	}
	if string(settledMsg.Key) != roomID {
		t.Errorf("settled message key = %q, want %q", settledMsg.Key, roomID)
	}

	var gotWager WagerPlaced
	if err := json.Unmarshal(wagerMsg.Value, &gotWager); err != nil {
		t.Fatalf("unmarshaling wager message: %v", err)
	}
	if gotWager != wager {
		t.Errorf("decoded wager = %+v, want %+v", gotWager, wager)
	}

	var gotSettled RoundSettled
	if err := json.Unmarshal(settledMsg.Value, &gotSettled); err != nil {
		t.Fatalf("unmarshaling settled message: %v", err)
	}
	if !reflect.DeepEqual(gotSettled, settled) {
		t.Errorf("decoded settled = %+v, want %+v (payouts must survive)", gotSettled, settled)
	}

	// Same room key must land both events on the same partition — that
	// co-location is what makes per-room ordering real, not nominal.
	if wagerMsg.Partition != settledMsg.Partition {
		t.Errorf("wager partition %d != settled partition %d, want equal (same room key)", wagerMsg.Partition, settledMsg.Partition)
	}
}

// readUntilKey scans a reader for the first message whose key matches
// want, ignoring other tests' messages sharing the topic.
func readUntilKey(t *testing.T, ctx context.Context, r *kafka.Reader, want string) kafka.Message {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		msg, err := r.ReadMessage(readCtx)
		cancel()
		if err != nil {
			continue
		}
		if string(msg.Key) == want {
			return msg
		}
	}
	t.Fatalf("no message with key %q read from topic %q before deadline", want, r.Config().Topic)
	return kafka.Message{}
}
