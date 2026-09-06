package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo provides read and write access to the double-entry ledger stored
// in PostgreSQL.
type Repo struct {
	pool *pgxpool.Pool
}

// New returns a new Repo using the given connection pool.
func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// WriteBatch persists every transaction and its entries inside one PostgreSQL
// transaction, skipping any whose idempotency_key is already present.
// The UNIQUE constraint on idempotency_key makes at-least-once Kafka delivery
// safe: replayed messages are absorbed silently via ON CONFLICT DO NOTHING,
// and the surrounding pgx transaction is never aborted mid-batch by a duplicate.
// Returns how many transactions were newly written.
func (r *Repo) WriteBatch(ctx context.Context, txns []Transaction) (written int, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("ledger: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, txn := range txns {
		// Insert the transaction row, skipping if idempotency_key is already present.
		var txnID uuid.UUID
		err = tx.QueryRow(ctx,
			`INSERT INTO transactions (id, idempotency_key, kind, room_id, round_id)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (idempotency_key) DO NOTHING
			 RETURNING id`,
			uuid.New(), txn.IdempotencyKey, txn.Kind, txn.RoomID, txn.RoundID).Scan(&txnID)
		if err != nil {
			if err == pgx.ErrNoRows {
				// This key is already applied; skip its entries and move on.
				continue
			}
			return 0, fmt.Errorf("ledger: inserting transaction: %w", err)
		}

		// Insert account rows for each entry (idempotent via ON CONFLICT).
		for _, entry := range txn.Entries {
			accountID := entry.Account.ID()
			var userID *string
			if entry.Account.UserID != "" {
				userID = &entry.Account.UserID
			}
			var roomID *string
			if entry.Account.RoomID != "" {
				roomID = &entry.Account.RoomID
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO accounts (id, kind, user_id, room_id)
				 VALUES ($1, $2, $3, $4)
				 ON CONFLICT (id) DO NOTHING`,
				accountID, string(entry.Account.Kind), userID, roomID)
			if err != nil {
				return 0, fmt.Errorf("ledger: inserting account: %w", err)
			}
		}

		// Insert ledger entries.
		for _, entry := range txn.Entries {
			accountID := entry.Account.ID()
			_, err := tx.Exec(ctx,
				`INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount)
				 VALUES ($1, $2, $3, $4, $5)`,
				uuid.New(), txnID, accountID, string(entry.Direction), entry.Amount)
			if err != nil {
				return 0, fmt.Errorf("ledger: inserting entry: %w", err)
			}
		}

		written++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("ledger: committing batch of %d: %w", len(txns), err)
	}

	return written, nil
}

// WalletBalancesForRoom returns the balance (Σ credits − Σ debits) per user_wallet
// account, restricted to transactions belonging to roomID. If a user has no entries,
// they do not appear in the map.
func (r *Repo) WalletBalancesForRoom(ctx context.Context, roomID string) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.user_id::text,
		        SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE -e.amount END)
		   FROM ledger_entries e
		   JOIN accounts     a ON a.id = e.account_id
		   JOIN transactions t ON t.id = e.transaction_id
		  WHERE a.kind = 'user_wallet' AND t.room_id = $1
		  GROUP BY a.user_id`,
		roomID)
	if err != nil {
		return nil, fmt.Errorf("ledger: querying wallet balances: %w", err)
	}
	defer rows.Close()

	balances := make(map[string]int64)
	for rows.Next() {
		var userID string
		var balance int64
		if err := rows.Scan(&userID, &balance); err != nil {
			return nil, fmt.Errorf("ledger: scanning wallet balance: %w", err)
		}
		balances[userID] = balance
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: iterating wallet balances: %w", err)
	}

	return balances, nil
}

// PoolBalance returns the balance (Σ credits − Σ debits) for a room's round_pool
// account. Returns 0 if no entries exist for this room.
func (r *Repo) PoolBalance(ctx context.Context, roomID string) (int64, error) {
	var balance *int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE -e.amount END), 0)
		   FROM ledger_entries e
		   JOIN accounts     a ON a.id = e.account_id
		   JOIN transactions t ON t.id = e.transaction_id
		  WHERE a.kind = 'round_pool' AND a.room_id = $1`,
		roomID).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("ledger: querying pool balance: %w", err)
	}

	if balance == nil {
		return 0, nil
	}
	return *balance, nil
}

// DustForRoom returns the total amount credited to the system_dust account
// for transactions belonging to roomID. Returns 0 if no dust exists for this room.
func (r *Repo) DustForRoom(ctx context.Context, roomID string) (int64, error) {
	var dust *int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(e.amount), 0)
		   FROM ledger_entries e
		   JOIN accounts     a ON a.id = e.account_id
		   JOIN transactions t ON t.id = e.transaction_id
		  WHERE a.kind = 'system_dust' AND t.room_id = $1`,
		roomID).Scan(&dust)
	if err != nil {
		return 0, fmt.Errorf("ledger: querying dust: %w", err)
	}

	if dust == nil {
		return 0, nil
	}
	return *dust, nil
}

// TransactionCount returns how many transactions belong to roomID.
func (r *Repo) TransactionCount(ctx context.Context, roomID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE room_id = $1`,
		roomID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("ledger: counting transactions: %w", err)
	}
	return count, nil
}
