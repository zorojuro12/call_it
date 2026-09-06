package migrate

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDSN points at callit_test — every test in this package runs
// against it, never the maintenance database or the dev database.
var testDSN string

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
		log.Fatalf("migrate: cannot connect to PostgreSQL at %s: %v — run `make up` and retry", maintenanceDSN, err)
	}
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("migrate: cannot reach PostgreSQL at %s: %v — run `make up` and retry", maintenanceDSN, err)
	}

	if _, err := pool.Exec(ctx, "DROP DATABASE IF EXISTS callit_test"); err != nil {
		log.Fatalf("migrate: DROP DATABASE callit_test failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE DATABASE callit_test"); err != nil {
		log.Fatalf("migrate: CREATE DATABASE callit_test failed: %v", err)
	}
	pool.Close()

	testDSN = replaceDBName(maintenanceDSN, "callit_test")

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
