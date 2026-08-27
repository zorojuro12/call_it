package ledger

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("cannot connect to test database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// TestMigration0002 verifies that migration 0002 creates the expected indexes
// and enforces the account identity constraints.
func TestMigration0002(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Check that all expected indexes exist.
	expectedIndexes := []string{
		"accounts_user_wallet_key",
		"accounts_round_pool_key",
		"accounts_system_singleton_key",
		"ledger_entries_transaction_id_idx",
		"ledger_entries_account_id_idx",
	}

	for _, idxName := range expectedIndexes {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)`,
			idxName).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check index %s: %v", idxName, err)
		}
		if !exists {
			t.Errorf("expected index %s not found", idxName)
		}
	}

	// Verify the unique constraint on user_wallet accounts.
	// Insert one user_wallet account.
	userID := uuid.NewString()
	id1 := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO accounts (id, kind, user_id) VALUES ($1, 'user_wallet', $2)`,
		id1, userID)
	if err != nil {
		t.Fatalf("failed to insert first account: %v", err)
	}

	// Try to insert a second account with different id but same user_id and kind.
	// This should fail with a unique constraint violation (23505).
	id2 := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO accounts (id, kind, user_id) VALUES ($1, 'user_wallet', $2)`,
		id2, userID)
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}

	pgErr, ok := err.(*pgconn.PgError)
	if !ok || pgErr.Code != "23505" {
		t.Fatalf("expected unique violation (23505), got: %v (type %T)", err, err)
	}
}
