package migrate

import (
	"context"
	"testing"

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

func TestUpCreatesLedgerTables(t *testing.T) {
	t.Parallel()

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
