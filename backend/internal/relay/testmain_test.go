package relay

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
)

// testDB mirrors redisstore's: DB 15, never 0.
const testDB = 15

var testRedisAddr string
var testClient *redis.Client

func TestMain(m *testing.M) {
	testRedisAddr = os.Getenv("REDIS_ADDR")
	if testRedisAddr == "" {
		testRedisAddr = "localhost:6379"
	}

	testClient = redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testDB,
	})

	if err := testClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("relay: cannot reach Redis at %s (db %d): %v — run `make up` and retry", testRedisAddr, testDB, err)
	}

	if err := testClient.FlushDB(context.Background()).Err(); err != nil {
		log.Fatalf("relay: FLUSHDB on db %d failed: %v", testDB, err)
	}

	code := m.Run()

	if err := testClient.Close(); err != nil {
		log.Fatalf("relay: closing test client failed: %v", err)
	}

	os.Exit(code)
}

var testIDCounter atomic.Uint64

// testStreamAndGroup returns a collision-free stream and group name pair
// so tests do not read each other's entries.
func testStreamAndGroup(t *testing.T) (stream, group string) {
	t.Helper()
	n := testIDCounter.Add(1)
	base := fmt.Sprintf("%s-%d", t.Name(), n)
	return "stream-" + base, "group-" + base
}
