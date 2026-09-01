package ledger

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// TestReconcileAfterLoad proves the Redis<->PostgreSQL reconciliation
// identity holds over state produced by a real k6 load run — thousands
// of wagers driven through the socket, relayed to Kafka, and consumed
// into PostgreSQL by the real cmd/relay and cmd/ledger-worker binaries —
// rather than a fixture this test builds itself. Closes parent plan §12's
// last unchecked money-correctness box.
//
// Deliberately connects to the LIVE stack, not this package's own
// callit_test database or Redis DB 15: TestMain drops and recreates
// callit_test and FLUSHDBs DB 15 on every run of this test binary
// (testmain_test.go), and the real load run's cmd/api, cmd/relay, and
// cmd/ledger-worker wrote to the default database and Redis DB 0
// instead (internal/config's RedisDB/POSTGRES_DSN defaults). Reading
// through the scratch connections would silently see empty state and
// pass over nothing.
//
// Skip semantics, deliberately narrow: CLAUDE.md requires internal/ledger's
// integration tests to fail rather than skip when a dependency is
// unreachable — this test keeps that rule where it applies. It skips
// only when RECONCILE_ROOM_IDS is unset, which means "no load run has
// happened in this environment," not "a dependency is down." Once the
// env var is set, an unreachable Redis or PostgreSQL fails the test.
func TestReconcileAfterLoad(t *testing.T) {
	raw := os.Getenv("RECONCILE_ROOM_IDS")
	if raw == "" {
		t.Skip("RECONCILE_ROOM_IDS not set — no load run to reconcile in this environment")
	}

	var roomIDs []string
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			roomIDs = append(roomIDs, id)
		}
	}
	if len(roomIDs) == 0 {
		t.Fatalf("RECONCILE_ROOM_IDS is set but contains no room IDs: %q", raw)
	}

	ctx := context.Background()

	liveDSN := os.Getenv("POSTGRES_DSN")
	if liveDSN == "" {
		liveDSN = "postgres://callit:callit@localhost:5432/callit?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, liveDSN)
	if err != nil {
		t.Fatalf("connect to live PostgreSQL at %s: %v", liveDSN, err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping live PostgreSQL at %s: %v", liveDSN, err)
	}
	repo := New(pool)

	liveRedisAddr := os.Getenv("REDIS_ADDR")
	if liveRedisAddr == "" {
		liveRedisAddr = "localhost:6379"
	}
	liveRedisDB := 0
	if v := os.Getenv("REDIS_DB"); v != "" {
		db, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("REDIS_DB %q is not a valid integer: %v", v, err)
		}
		liveRedisDB = db
	}
	store, err := redisstore.New(liveRedisAddr, liveRedisDB)
	if err != nil {
		t.Fatalf("connect to live Redis at %s db %d: %v", liveRedisAddr, liveRedisDB, err)
	}
	defer store.Close()

	// A second, unwrapped client for the one read redisstore.Store has
	// no public method for: listing every user_id in a room's wallet
	// hash. Every other read below goes through Store's own exported
	// methods (Balance, OpeningStake), matching the existing
	// reconciliation tests' helpers rather than a second parsing path.
	rawRedis := redis.NewClient(&redis.Options{Addr: liveRedisAddr, DB: liveRedisDB})
	defer rawRedis.Close()

	for _, roomID := range roomIDs {
		roomID := roomID
		t.Run(roomID, func(t *testing.T) {
			assertWalletIdentity(t, ctx, rawRedis, store, repo, roomID)
			assertTransactionsBalance(t, ctx, pool, roomID)
			assertWagerCount(t, ctx, pool, roomID)
		})
	}
}

// assertWalletIdentity checks, for every user with a wallet in roomID,
// that redis_wallet(user, room) - opening_stake(user, room) ==
// ledger_balance(user, room) — the same identity and the same
// redisstore.Store / Repo helpers TestReconcileSequential and
// TestReconcileConcurrent already use over fixtures they build
// themselves. A user with no ledger entries (the host, who cannot
// wager, or a player who joined but never wagered) is not an error by
// itself — only a missing ledger entry alongside a Redis balance that
// has actually moved from the opening stake is, since that is a wager
// the ledger lost.
func assertWalletIdentity(t *testing.T, ctx context.Context, rawRedis *redis.Client, store *redisstore.Store, repo *Repo, roomID string) {
	t.Helper()

	wallets, err := rawRedis.HGetAll(ctx, redisstore.RoomWalletsKey(roomID)).Result()
	if err != nil {
		t.Fatalf("HGETALL wallets for room %s: %v", roomID, err)
	}
	if len(wallets) == 0 {
		t.Fatalf("room %s has no wallets in Redis — wrong room ID, or Redis state has moved on since the load run", roomID)
	}

	balances, err := repo.WalletBalancesForRoom(ctx, roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom(%s): %v", roomID, err)
	}

	for userID := range wallets {
		redisBal, err := store.Balance(ctx, roomID, userID)
		if err != nil {
			t.Fatalf("Balance(%s, %s): %v", roomID, userID, err)
		}
		opening, err := store.OpeningStake(ctx, roomID, userID)
		if err != nil {
			t.Fatalf("OpeningStake(%s, %s): %v", roomID, userID, err)
		}
		want := int64(redisBal) - int64(opening)

		ledgerBal, ok := balances[userID]
		if !ok {
			if want == 0 {
				continue // no wager activity for this user (e.g. the host) — nothing to reconcile
			}
			t.Errorf("player %s: no ledger balance found, but redis balance %d != opening stake %d — a missing key here is a lost wager, not a zero balance", userID, redisBal, opening)
			continue
		}
		if ledgerBal != want {
			t.Errorf("player %s: ledger balance = %d, want %d (redis %d - opening %d)", userID, ledgerBal, want, redisBal, opening)
		}
	}
}

// assertTransactionsBalance re-proves, over the room's real
// load-generated rows, the invariant assert_transaction_balanced()
// already enforces at INSERT time via a DEFERRABLE constraint trigger
// (migrations/0001_ledger_schema.up.sql) — making the invariant visible
// against real data rather than trusting it silently held.
func assertTransactionsBalance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, roomID string) {
	t.Helper()

	rows, err := pool.Query(ctx,
		`SELECT t.id, COALESCE(SUM(CASE WHEN e.direction = 'debit' THEN e.amount ELSE -e.amount END), 0) AS net
		   FROM transactions t
		   LEFT JOIN ledger_entries e ON e.transaction_id = t.id
		  WHERE t.room_id = $1
		  GROUP BY t.id
		 HAVING COALESCE(SUM(CASE WHEN e.direction = 'debit' THEN e.amount ELSE -e.amount END), 0) <> 0`,
		roomID)
	if err != nil {
		t.Fatalf("querying transaction balances for room %s: %v", roomID, err)
	}
	defer rows.Close()

	var unbalanced []string
	for rows.Next() {
		var id string
		var net int64
		if err := rows.Scan(&id, &net); err != nil {
			t.Fatalf("scanning unbalanced transaction for room %s: %v", roomID, err)
		}
		unbalanced = append(unbalanced, fmt.Sprintf("%s (net %d)", id, net))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating transaction balances for room %s: %v", roomID, err)
	}
	if len(unbalanced) > 0 {
		t.Errorf("room %s has %d unbalanced transactions (Σdebits != Σcredits): %v", roomID, len(unbalanced), unbalanced)
	}
}

// assertWagerCount checks the room's wager-kind transaction count
// against the count of wallet-debit entries those same transactions
// produced — every accepted wager debits exactly one user_wallet
// account once, so the two counts must agree. A relay or worker bug
// that wrote a transaction row without its entry (or the reverse) would
// desynchronize them even though both sides individually satisfy
// assert_transactions_balance's per-transaction check above.
//
// Also enforces the plan's "at least 1,000 wagers" floor — a pass over
// a handful of wagers would not be evidence this box needed: the point
// of this gate is a real, load-scale run.
func assertWagerCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, roomID string) {
	t.Helper()

	var wagerTxns int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE room_id = $1 AND kind = 'wager'`,
		roomID).Scan(&wagerTxns); err != nil {
		t.Fatalf("counting wager transactions for room %s: %v", roomID, err)
	}

	var walletDebits int
	if err := pool.QueryRow(ctx,
		`SELECT count(*)
		   FROM ledger_entries e
		   JOIN accounts a ON a.id = e.account_id
		   JOIN transactions t ON t.id = e.transaction_id
		  WHERE t.room_id = $1 AND t.kind = 'wager' AND a.kind = 'user_wallet' AND e.direction = 'debit'`,
		roomID).Scan(&walletDebits); err != nil {
		t.Fatalf("counting wallet-debit entries for room %s: %v", roomID, err)
	}

	if wagerTxns != walletDebits {
		t.Errorf("room %s: %d wager transactions but %d wallet-debit entries — every accepted wager must debit exactly one wallet once", roomID, wagerTxns, walletDebits)
	}
	if wagerTxns < 1000 {
		t.Errorf("room %s: only %d wager transactions — this gate requires at least 1,000 to be evidence of reconciliation holding under load, not just under a fixture", roomID, wagerTxns)
	}
}
