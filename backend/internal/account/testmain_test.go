package account

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

// testDB mirrors redisstore's testmain_test.go: DB 15, never 0, so a run
// against this suite can't touch local dev state.
const testDB = 15

var testRedisAddr string

// TestMain mirrors redisstore's: DB 15, fail-not-skip on unreachable
// Redis, FLUSHDB for a clean slate. Safe against internal/redisstore's
// identical TestMain sharing DB 15 only because the Makefile's `go test`
// invocations run with -p 1 — Go's default package-level parallelism
// would otherwise let two independent TestMains race to flush the same
// live DB, each capable of wiping the other package's in-flight test
// data mid-run.
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
		log.Fatalf("account: cannot reach Redis at %s (db %d): %v — run `make up` and retry", testRedisAddr, testDB, err)
	}

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		log.Fatalf("account: FLUSHDB on db %d failed: %v", testDB, err)
	}

	if err := client.Close(); err != nil {
		log.Fatalf("account: closing setup client failed: %v", err)
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
