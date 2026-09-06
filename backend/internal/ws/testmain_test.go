package ws

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// testDB mirrors redisstore's testmain_test.go: DB 15, never 0. Added
// in Task 10 — every earlier ws test was pure in-memory (hub/room/
// client), but the end-to-end test (internal/ws/e2e_test.go, package
// ws_test) needs a real store behind real round/wager services, and
// this TestMain governs the whole test binary regardless of which
// file in this directory it runs from.
const testDB = 15

func TestMain(m *testing.M) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   testDB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("ws: cannot reach Redis at %s (db %d): %v — run `make up` and retry", addr, testDB, err)
	}

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		log.Fatalf("ws: FLUSHDB on db %d failed: %v", testDB, err)
	}

	if err := client.Close(); err != nil {
		log.Fatalf("ws: closing setup client failed: %v", err)
	}

	os.Exit(m.Run())
}
