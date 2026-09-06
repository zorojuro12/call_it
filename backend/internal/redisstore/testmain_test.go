package redisstore

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
)

// testDB is the database index every integration test in this package
// uses. Never 0 — a run against this suite must not be able to touch
// local dev state.
const testDB = 15

var testRedisAddr string

func TestMain(m *testing.M) {
	testRedisAddr = os.Getenv("REDIS_ADDR")
	if testRedisAddr == "" {
		testRedisAddr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testDB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redisstore: cannot reach Redis at %s (db %d): %v — run `make up` and retry", testRedisAddr, testDB, err)
	}

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		log.Fatalf("redisstore: FLUSHDB on db %d failed: %v", testDB, err)
	}

	if err := client.Close(); err != nil {
		log.Fatalf("redisstore: closing setup client failed: %v", err)
	}

	os.Exit(m.Run())
}

var testIDCounter atomic.Uint64

// testID returns a collision-free identifier derived from the test's name
// and an atomic counter, so no two tests touch the same room or round keys.
func testID(t *testing.T, kind string) string {
	t.Helper()
	n := testIDCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", kind, t.Name(), n)
}

// testOutcomes returns n placeholder outcome labels for CreateRound
// calls that only care about the count, not the labels' content.
func testOutcomes(n int) []string {
	outcomes := make([]string, n)
	for i := range outcomes {
		outcomes[i] = fmt.Sprintf("outcome-%d", i)
	}
	return outcomes
}

// newTestStore returns a Store bound to the test database with a
// per-test outbox stream name, so concurrent tests never read each
// other's events. It is closed automatically via t.Cleanup.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(testRedisAddr, testDB)
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	store.outboxStream = testID(t, "outbox")

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("newTestStore cleanup: closing store: %v", err)
		}
	})

	return store
}

func TestStorePing(t *testing.T) {
	store := newTestStore(t)

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() = %v, want nil", err)
	}
}
