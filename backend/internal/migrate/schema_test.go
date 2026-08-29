package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// wantColumns is the minimal column set Task 1 Checkpoint 1 pins per
// table. schema_migrations (golang-migrate's own bookkeeping table) is
// deliberately not asserted against — only presence of these three.
var wantColumns = map[string][]string{
	"accounts":       {"id", "kind", "user_id", "room_id", "created_at"},
	"transactions":   {"id", "idempotency_key", "kind", "room_id", "round_id", "occurred_at"},
	"ledger_entries": {"id", "transaction_id", "account_id", "direction", "amount"},
}

// Tests in this package are deliberately not t.Parallel(): every test
// shares one callit_test database and mutates its schema (Up/Down create
// and drop the same tables), unlike redisstore's per-key isolation.
func TestUpCreatesLedgerTables(t *testing.T) {
	ctx := context.Background()
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() error = %v, want nil", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	for table, wantCols := range wantColumns {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("checking table %s exists: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s: not found in public schema", table)
		}

		rows, err := pool.Query(ctx,
			`SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1`,
			table,
		)
		if err != nil {
			t.Fatalf("listing columns for %s: %v", table, err)
		}
		got := map[string]bool{}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatalf("scanning column for %s: %v", table, err)
			}
			got[col] = true
		}
		rows.Close()

		for _, col := range wantCols {
			if !got[col] {
				t.Errorf("table %s: missing expected column %q (has %v)", table, col, got)
			}
		}
	}
}

func tableExists(t *testing.T, ctx context.Context, dsn, table string) bool {
	t.Helper()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		table,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("checking table %s exists: %v", table, err)
	}
	return exists
}

func TestDownRemovesLedgerTables(t *testing.T) {
	ctx := context.Background()
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() error = %v, want nil", err)
	}

	if err := Down(ctx, testDSN); err != nil {
		t.Fatalf("Down() error = %v, want nil", err)
	}

	for table := range wantColumns {
		if tableExists(t, ctx, testDSN, table) {
			t.Errorf("table %s: still present after Down()", table)
		}
	}

	// Down must leave a re-migratable database, not a wedged one.
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() after Down() error = %v, want nil", err)
	}
	for table := range wantColumns {
		if !tableExists(t, ctx, testDSN, table) {
			t.Errorf("table %s: missing after re-Up()", table)
		}
	}
}

// seedAccountAndTransaction inserts two accounts and one transaction row,
// returning (accountA, accountB, transactionID) for ledger_entries tests
// that need valid foreign keys already in place.
func seedAccountAndTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	acctA, acctB, txID := uuid.New(), uuid.New(), uuid.New()

	// acctB uses 'room_escrow', not 'system_dust': migration 0002 makes
	// system_dust a global singleton via accounts_system_singleton_key, and
	// this helper is called multiple times per test run. room_escrow carries
	// no such constraint and this helper only needs a second distinct account.
	_, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, kind) VALUES ($1, 'user_wallet'), ($2, 'room_escrow')`,
		acctA, acctB,
	)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (id, idempotency_key, kind) VALUES ($1, $2, 'settlement')`,
		txID, txID.String(),
	)
	if err != nil {
		t.Fatalf("seeding transaction: %v", err)
	}

	return acctA, acctB, txID
}

func TestUnbalancedTransactionRejectedAtCommit(t *testing.T) {
	ctx := context.Background()
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := Down(ctx, testDSN); err != nil {
			t.Fatalf("cleanup Down() error = %v", err)
		}
	})

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	t.Run("single-sided entry rejected at commit", func(t *testing.T) {
		acctA, _, txID := seedAccountAndTransaction(t, ctx, pool)

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}

		// The INSERT itself must succeed — the trigger is deferred, not
		// immediate. An immediate trigger would reject the first leg of
		// every legitimate two-leg entry.
		_, err = tx.Exec(ctx,
			`INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount) VALUES ($1, $2, $3, 'debit', 100)`,
			uuid.New(), txID, acctA,
		)
		if err != nil {
			t.Fatalf("INSERT (single leg) error = %v, want nil (trigger must be deferred)", err)
		}

		err = tx.Commit(ctx)
		if err == nil {
			t.Fatalf("Commit() error = nil, want an unbalanced-transaction error")
		}
		if !strings.Contains(err.Error(), "transaction is not balanced") {
			t.Errorf("Commit() error = %v, want it to mention %q", err, "transaction is not balanced")
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transaction_id = $1`, txID).Scan(&count); err != nil {
			t.Fatalf("counting ledger_entries: %v", err)
		}
		if count != 0 {
			t.Errorf("ledger_entries rows for failed transaction = %d, want 0", count)
		}
	})

	t.Run("balanced pair commits successfully", func(t *testing.T) {
		acctA, acctB, txID := seedAccountAndTransaction(t, ctx, pool)

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount) VALUES ($1, $2, $3, 'debit', 100)`,
			uuid.New(), txID, acctA,
		)
		if err != nil {
			t.Fatalf("INSERT (debit leg) error = %v", err)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount) VALUES ($1, $2, $3, 'credit', 100)`,
			uuid.New(), txID, acctB,
		)
		if err != nil {
			t.Fatalf("INSERT (credit leg) error = %v", err)
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit() error = %v, want nil for a balanced pair", err)
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transaction_id = $1`, txID).Scan(&count); err != nil {
			t.Fatalf("counting ledger_entries: %v", err)
		}
		if count != 2 {
			t.Errorf("ledger_entries rows for balanced transaction = %d, want 2", count)
		}
	})
}

func TestDuplicateIdempotencyKeyRejected(t *testing.T) {
	ctx := context.Background()
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() error = %v, want nil", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	key := uuid.New().String()

	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (id, idempotency_key, kind) VALUES ($1, $2, 'settlement')`,
		uuid.New(), key,
	)
	if err != nil {
		t.Fatalf("first insert error = %v, want nil", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (id, idempotency_key, kind) VALUES ($1, $2, 'settlement')`,
		uuid.New(), key,
	)
	if err == nil {
		t.Fatalf("second insert with duplicate idempotency_key error = nil, want a unique_violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("second insert error = %v, want SQLSTATE 23505 (unique_violation)", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE idempotency_key = $1`, key).Scan(&count); err != nil {
		t.Fatalf("counting transactions: %v", err)
	}
	if count != 1 {
		t.Errorf("transactions rows with key %q = %d, want 1", key, count)
	}
}

func TestNonPositiveAmountRejected(t *testing.T) {
	ctx := context.Background()
	if err := Up(ctx, testDSN); err != nil {
		t.Fatalf("Up() error = %v, want nil", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	tests := []struct {
		name   string
		amount int64
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acctA, _, txID := seedAccountAndTransaction(t, ctx, pool)

			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("Begin() error = %v", err)
			}
			defer tx.Rollback(ctx)

			_, err = tx.Exec(ctx,
				`INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount) VALUES ($1, $2, $3, 'debit', $4)`,
				uuid.New(), txID, acctA, tt.amount,
			)
			if err == nil {
				t.Fatalf("INSERT with amount %d error = nil, want a check_violation", tt.amount)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("INSERT with amount %d error = %v, want SQLSTATE 23514 (check_violation)", tt.amount, err)
			}
		})
	}
}
