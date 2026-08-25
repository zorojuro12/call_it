package httpapi

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// testDB mirrors redisstore's testmain_test.go: DB 15, never 0.
const testDB = 15

var testRedisAddr string

// TestMain mirrors redisstore's, including FLUSHDB — safe here only
// because the Makefile's go test invocations run with -p 1 (see
// internal/account/testmain_test.go for why multiple packages sharing
// DB 15 need that).
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
		log.Fatalf("httpapi: cannot reach Redis at %s (db %d): %v — run `make up` and retry", testRedisAddr, testDB, err)
	}

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		log.Fatalf("httpapi: FLUSHDB on db %d failed: %v", testDB, err)
	}

	if err := client.Close(); err != nil {
		log.Fatalf("httpapi: closing setup client failed: %v", err)
	}

	os.Exit(m.Run())
}

var testIDCounter atomic.Uint64

// testID returns a collision-free identifier derived from the test's
// name and an atomic counter, so no two tests touch the same keys.
func testID(t *testing.T, kind string) string {
	t.Helper()
	n := testIDCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", kind, t.Name(), n)
}

// newTestStore returns a redisstore.Store bound to the test database,
// closed automatically via t.Cleanup.
func newTestStore(t *testing.T) *redisstore.Store {
	t.Helper()

	store, err := redisstore.New(testRedisAddr, testDB)
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("newTestStore cleanup: closing store: %v", err)
		}
	})

	return store
}
