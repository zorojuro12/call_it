package ledger

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"

	"github.com/zorojuro12/call_it/backend/internal/migrate"
)

// testDSN points at callit_test — every test in this package runs
// against it, never the maintenance database or the dev database.
var testDSN string

// testRedisAddr and testKafkaBrokers are read by reconcile_test.go's
// fixture, which needs its own redisstore.Store and relay/Kafka
// connections on top of the PostgreSQL setup below. DB 15, never DB 0 —
// the same convention redisstore and relay's own suites use.
const testRedisDB = 15

var testRedisAddr string
var testKafkaBrokers []string

// TestMain mirrors redisstore's DB-15 convention: these tests fail
// rather than skip when PostgreSQL is unreachable, since a suite whose
// whole purpose is proving the ledger schema is correct must not report
// PASS while executing nothing.
func TestMain(m *testing.M) {
	maintenanceDSN := os.Getenv("POSTGRES_DSN")
	if maintenanceDSN == "" {
		maintenanceDSN = "postgres://callit:callit@localhost:5432/callit?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, maintenanceDSN)
	if err != nil {
		log.Fatalf("ledger: cannot connect to PostgreSQL at %s: %v — run `make up` and retry", maintenanceDSN, err)
	}
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ledger: cannot reach PostgreSQL at %s: %v — run `make up` and retry", maintenanceDSN, err)
	}

	if _, err := pool.Exec(ctx, "DROP DATABASE IF EXISTS callit_test"); err != nil {
		log.Fatalf("ledger: DROP DATABASE callit_test failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE DATABASE callit_test"); err != nil {
		log.Fatalf("ledger: CREATE DATABASE callit_test failed: %v", err)
	}
	pool.Close()

	testDSN = replaceDBName(maintenanceDSN, "callit_test")

	// Apply all migrations to callit_test.
	migratePool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		log.Fatalf("ledger: cannot connect to callit_test at %s: %v", testDSN, err)
	}
	if err := migrate.Up(ctx, testDSN); err != nil {
		log.Fatalf("ledger: migrate.Up failed: %v", err)
	}
	migratePool.Close()

	// Redis probe (DB 15) — reconcile_test.go's fixture drives a real
	// redisstore.Store and relay through this database. Fail rather than
	// skip: the reconciliation suite's whole purpose is proving zero
	// double-spend across Redis and PostgreSQL, so it must not report
	// PASS while executing nothing.
	testRedisAddr = os.Getenv("REDIS_ADDR")
	if testRedisAddr == "" {
		testRedisAddr = "localhost:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("ledger: cannot reach Redis at %s (db %d): %v — run `make up` and retry", testRedisAddr, testRedisDB, err)
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		log.Fatalf("ledger: FLUSHDB on db %d failed: %v", testRedisDB, err)
	}
	if err := redisClient.Close(); err != nil {
		log.Fatalf("ledger: closing Redis probe client failed: %v", err)
	}

	// Kafka probe — a bare TCP dial to the first broker, mirroring
	// internal/events' TestMain.
	kafkaAddr := os.Getenv("KAFKA_BROKERS")
	if kafkaAddr == "" {
		kafkaAddr = "localhost:9092"
	}
	testKafkaBrokers = strings.Split(kafkaAddr, ",")
	kconn, err := kafka.DialContext(ctx, "tcp", testKafkaBrokers[0])
	if err != nil {
		log.Fatalf("ledger: cannot reach Kafka at %s: %v — run `make up-full` and retry", testKafkaBrokers[0], err)
	}
	if err := kconn.Close(); err != nil {
		log.Fatalf("ledger: closing Kafka dial probe failed: %v", err)
	}

	os.Exit(m.Run())
}

// replaceDBName swaps the path component of a postgres DSN of the form
// postgres://user:pass@host:port/dbname?params, leaving user, host, and
// params untouched.
func replaceDBName(dsn, newDB string) string {
	const prefix = "postgres://"
	rest := strings.TrimPrefix(dsn, prefix)

	slash := strings.IndexByte(rest, '/')
	authority := rest[:slash]
	tail := rest[slash+1:]

	query := ""
	if q := strings.IndexByte(tail, '?'); q >= 0 {
		query = tail[q:]
	}

	return fmt.Sprintf("%s%s/%s%s", prefix, authority, newDB, query)
}
