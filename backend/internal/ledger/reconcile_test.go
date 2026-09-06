package ledger

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/events"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/relay"
)

// Task 6 exercises the whole Redis -> Kafka -> PostgreSQL pipeline built
// in Tasks 1-5 against a real, settled round. Every component under test
// was already unit-tested in isolation; these checkpoints are declared
// PASS-on-first-run in the plan, not RED->GREEN cycles.

const (
	reconcilePlayers         = 8
	reconcileWagersPerPlayer = 5
	reconcileWagerAmount     = domain.Tokens(10)
	reconcileBuyIn           = domain.Tokens(100)
)

// reconcileFixture is the scenario shared by Checkpoints 1-3: a room with
// 8 joined players who have each wagered 5 times on a round that is then
// locked and settled on outcome 0. Every ID is uuid.NewString() —
// PostgreSQL's uuid columns reject redisstore's testID() helper.
type reconcileFixture struct {
	store      *redisstore.Store
	roomID     string
	roundID    string
	hostID     string
	playerIDs  []string
	settlement domain.Settlement
}

// setupReconcileFixture creates the room, joins reconcilePlayers players,
// creates a round, places every wager (sequentially, or across
// reconcilePlayers goroutines when concurrent is true), locks, and
// settles on outcome 0. It does not touch Kafka or PostgreSQL.
func setupReconcileFixture(t *testing.T, ctx context.Context, concurrent bool) reconcileFixture {
	t.Helper()

	store, err := redisstore.New(testRedisAddr, testRedisDB)
	if err != nil {
		t.Fatalf("redisstore.New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	roomID := uuid.NewString()
	hostID := uuid.NewString()
	if err := store.CreateRoom(ctx, roomID, uuid.NewString(), hostID, reconcileBuyIn); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	playerIDs := make([]string, reconcilePlayers)
	for i := range playerIDs {
		playerIDs[i] = uuid.NewString()
		if _, err := store.JoinRoom(ctx, roomID, playerIDs[i], domain.StartingBalance); err != nil {
			t.Fatalf("JoinRoom() player %d error = %v", i, err)
		}
	}

	roundID := uuid.NewString()
	if err := store.CreateRound(ctx, roundID, roomID, "q?", []string{"a", "b"}, time.Now().Add(30*time.Second)); err != nil {
		t.Fatalf("CreateRound() error = %v", err)
	}

	placeWager := func(playerIdx, wagerIdx int) error {
		_, err := store.PlaceWager(ctx, redisstore.WagerRequest{
			RoomID:         roomID,
			RoundID:        roundID,
			UserID:         playerIDs[playerIdx],
			Outcome:        playerIdx % 2,
			Amount:         reconcileWagerAmount,
			IdempotencyKey: uuid.NewString(),
		})
		return err
	}

	if concurrent {
		var wg sync.WaitGroup
		errCh := make(chan error, reconcilePlayers*reconcileWagersPerPlayer)
		for i := 0; i < reconcilePlayers; i++ {
			wg.Add(1)
			go func(playerIdx int) {
				defer wg.Done()
				for w := 0; w < reconcileWagersPerPlayer; w++ {
					if err := placeWager(playerIdx, w); err != nil {
						errCh <- err
					}
				}
			}(i)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatalf("PlaceWager() (concurrent) error = %v", err)
		}
	} else {
		for i := 0; i < reconcilePlayers; i++ {
			for w := 0; w < reconcileWagersPerPlayer; w++ {
				if err := placeWager(i, w); err != nil {
					t.Fatalf("PlaceWager() error = %v", err)
				}
			}
		}
	}

	if err := store.LockRound(ctx, roundID); err != nil {
		t.Fatalf("LockRound() error = %v", err)
	}
	settlement, err := store.SettleRound(ctx, roundID, 0, uuid.NewString())
	if err != nil {
		t.Fatalf("SettleRound() error = %v", err)
	}

	return reconcileFixture{
		store:      store,
		roomID:     roomID,
		roundID:    roundID,
		hostID:     hostID,
		playerIDs:  playerIDs,
		settlement: settlement,
	}
}

// setupReconcileRelay builds and prepares a relay reading the shared
// wager-outbox stream under the production group and a fresh consumer
// name, so distinct test runs never collide on pending entries.
func setupReconcileRelay(t *testing.T, ctx context.Context) *relay.Relay {
	t.Helper()

	redisClient := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("relay redis client Ping() error = %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	producer := events.NewKafkaProducer(testKafkaBrokers)
	t.Cleanup(func() { _ = producer.Close() })
	if err := producer.EnsureTopics(ctx, events.Partitions); err != nil {
		t.Fatalf("EnsureTopics() error = %v", err)
	}

	r := relay.New(redisClient, redisstore.OutboxStream, redisstore.OutboxGroup, uuid.NewString(), producer)
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() error = %v", err)
	}
	return r
}

// setupReconcileWorker builds a ledger worker on a unique consumer group
// reading both topics from the beginning. The unique group is
// deliberate: the local Kafka topics retain messages from earlier runs,
// so this worker will also consume and ledger those. That is harmless —
// they dedupe on idempotency_key and belong to other room_ids — because
// every assertion in this file is scoped to its own fixture's roomID.
func setupReconcileWorker(t *testing.T, repo *Repo) *Worker {
	t.Helper()

	group := "ledger-recon-" + uuid.NewString()
	consumer := events.NewKafkaConsumer(testKafkaBrokers, group, []string{events.TopicWagersPlaced, events.TopicRoundsSettled}, true)
	t.Cleanup(func() { _ = consumer.Close() })
	return NewWorker(consumer, repo)
}

// drainReconcile empties the outbox to Kafka via the relay, then drains
// Kafka to PostgreSQL via the worker until roomID holds exactly
// wantTxns transactions. Bounded by a 60-second deadline so a stuck
// pipeline fails the test instead of hanging the suite.
func drainReconcile(t *testing.T, ctx context.Context, r *relay.Relay, worker *Worker, repo *Repo, roomID string, wantTxns int) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	consecutiveEmpty := 0
	for consecutiveEmpty < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("drain: relay did not empty the outbox within 60s")
		}
		n, err := r.Once(ctx, 100, 200*time.Millisecond)
		if err != nil {
			t.Fatalf("relay.Once() error = %v", err)
		}
		if n == 0 {
			consecutiveEmpty++
		} else {
			consecutiveEmpty = 0
		}
	}

	for {
		count, err := repo.TransactionCount(ctx, roomID)
		if err != nil {
			t.Fatalf("TransactionCount() error = %v", err)
		}
		if count == wantTxns {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("drain: TransactionCount(%s) = %d after 60s, want %d", roomID, count, wantTxns)
		}
		// 30s, not 2s: Once's context bounds both the Kafka fetch and the
		// batched PostgreSQL write, and a batch near maxBatch (100) can
		// take over a second of round trips against a local database.
		onceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if _, err := worker.Once(onceCtx); err != nil {
			cancel()
			t.Fatalf("worker.Once() error = %v", err)
		}
		cancel()
	}
}

// TestReconcileSequential places 40 sequential wagers, settles the
// round, drains Redis -> Kafka -> PostgreSQL, then asserts the
// reconciliation identity for every player:
//
//	redis_wallet - opening_stake == ledger_balance
//
// D2: the ledger records outbox movements only, so a user_wallet
// balance is a net session delta — the opening stake never reaches the
// outbox (Store.JoinRoom is a Go pipeline, not a Lua script), which is
// why this is not a direct comparison against the absolute Redis wallet.
func TestReconcileSequential(t *testing.T) {
	ctx := context.Background()
	pool := getTestPool(t)
	repo := New(pool)

	fx := setupReconcileFixture(t, ctx, false)
	r := setupReconcileRelay(t, ctx)
	worker := setupReconcileWorker(t, repo)

	wantTxns := reconcilePlayers*reconcileWagersPerPlayer + 1
	drainReconcile(t, ctx, r, worker, repo, fx.roomID, wantTxns)

	balances, err := repo.WalletBalancesForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom() error = %v", err)
	}

	for _, userID := range fx.playerIDs {
		redisBal, err := fx.store.Balance(ctx, fx.roomID, userID)
		if err != nil {
			t.Fatalf("Balance(%s) error = %v", userID, err)
		}
		opening, err := fx.store.OpeningStake(ctx, fx.roomID, userID)
		if err != nil {
			t.Fatalf("OpeningStake(%s) error = %v", userID, err)
		}

		ledgerBal, ok := balances[userID]
		if !ok {
			t.Fatalf("WalletBalancesForRoom missing player %s — a missing key is a lost wager, not a zero balance", userID)
		}

		want := int64(redisBal) - int64(opening)
		if ledgerBal != want {
			t.Errorf("player %s: ledger balance = %d, want %d (redis %d - opening %d)", userID, ledgerBal, want, redisBal, opening)
		}
	}
}

// TestReconcileConcurrent is the double-spend proof: place_wager.lua is
// atomic, the outbox XADD is inside that same atomic unit, and
// idempotency_key is unique per wager, so 40 concurrent wagers across 8
// goroutines must produce exactly 40 wager transactions plus 1
// settlement — never more (a duplicate slipped the unique constraint)
// and never fewer (one was lost).
func TestReconcileConcurrent(t *testing.T) {
	ctx := context.Background()
	pool := getTestPool(t)
	repo := New(pool)

	fx := setupReconcileFixture(t, ctx, true)
	r := setupReconcileRelay(t, ctx)
	worker := setupReconcileWorker(t, repo)

	wantTxns := reconcilePlayers*reconcileWagersPerPlayer + 1
	drainReconcile(t, ctx, r, worker, repo, fx.roomID, wantTxns)

	count, err := repo.TransactionCount(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("TransactionCount() error = %v", err)
	}
	if count != wantTxns {
		t.Fatalf("TransactionCount(%s) = %d, want exactly %d", fx.roomID, count, wantTxns)
	}

	balances, err := repo.WalletBalancesForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom() error = %v", err)
	}

	for _, userID := range fx.playerIDs {
		redisBal, err := fx.store.Balance(ctx, fx.roomID, userID)
		if err != nil {
			t.Fatalf("Balance(%s) error = %v", userID, err)
		}
		opening, err := fx.store.OpeningStake(ctx, fx.roomID, userID)
		if err != nil {
			t.Fatalf("OpeningStake(%s) error = %v", userID, err)
		}

		ledgerBal, ok := balances[userID]
		if !ok {
			t.Fatalf("WalletBalancesForRoom missing player %s — a missing key is a lost wager, not a zero balance", userID)
		}

		want := int64(redisBal) - int64(opening)
		if ledgerBal != want {
			t.Errorf("player %s: ledger balance = %d, want %d (redis %d - opening %d)", userID, ledgerBal, want, redisBal, opening)
		}
	}
}

// TestReconcileConservation asserts the round_pool account returns to
// zero and dust is exactly what domain.Settle computed. A lost
// wager_placed event leaves the pool short by that amount and a
// duplicated one leaves it long — neither necessarily shows up in a
// per-user wallet comparison if the corresponding settlement credit is
// also affected, but both show up here.
func TestReconcileConservation(t *testing.T) {
	ctx := context.Background()
	pool := getTestPool(t)
	repo := New(pool)

	fx := setupReconcileFixture(t, ctx, true)
	r := setupReconcileRelay(t, ctx)
	worker := setupReconcileWorker(t, repo)

	wantTxns := reconcilePlayers*reconcileWagersPerPlayer + 1
	drainReconcile(t, ctx, r, worker, repo, fx.roomID, wantTxns)

	poolBal, err := repo.PoolBalance(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("PoolBalance() error = %v", err)
	}
	if poolBal != 0 {
		t.Errorf("PoolBalance(%s) = %d, want 0 — every token that entered the round's pool must leave it", fx.roomID, poolBal)
	}

	dust, err := repo.DustForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("DustForRoom() error = %v", err)
	}
	if dust != int64(fx.settlement.Dust) {
		t.Errorf("DustForRoom(%s) = %d, want %d (domain.Settlement.Dust)", fx.roomID, dust, int64(fx.settlement.Dust))
	}

	balances, err := repo.WalletBalancesForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom() error = %v", err)
	}
	var walletSum int64
	for _, bal := range balances {
		walletSum += bal
	}

	if total := walletSum + poolBal + dust; total != 0 {
		t.Errorf("token conservation for room %s: Σwallets(%d) + pool(%d) + dust(%d) = %d, want 0", fx.roomID, walletSum, poolBal, dust, total)
	}
}

// drainWorkerUntilEmpty loops worker.Once until it reports two
// consecutive zero-message cycles, bounded by a 60-second deadline —
// the same "empty" signal drainReconcile uses for the relay, applied to
// a worker with nothing left upstream to fetch.
func drainWorkerUntilEmpty(t *testing.T, ctx context.Context, worker *Worker) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	consecutiveEmpty := 0
	for consecutiveEmpty < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("drainWorkerUntilEmpty: worker did not empty within 60s")
		}
		onceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		n, err := worker.Once(onceCtx)
		cancel()
		if err != nil {
			t.Fatalf("worker.Once() error = %v", err)
		}
		if n == 0 {
			consecutiveEmpty++
		} else {
			consecutiveEmpty = 0
		}
	}
}

// TestReconcileReplay is the at-least-once guarantee end to end: Kafka
// may redeliver any message any number of times, and the
// idempotency_key UNIQUE constraint is the single mechanism that
// absorbs it. A second worker on a fresh consumer group re-reads every
// message from the beginning of both topics and must change nothing.
func TestReconcileReplay(t *testing.T) {
	ctx := context.Background()
	pool := getTestPool(t)
	repo := New(pool)

	fx := setupReconcileFixture(t, ctx, true)
	r := setupReconcileRelay(t, ctx)
	worker := setupReconcileWorker(t, repo)

	wantTxns := reconcilePlayers*reconcileWagersPerPlayer + 1
	drainReconcile(t, ctx, r, worker, repo, fx.roomID, wantTxns)

	wantCount, err := repo.TransactionCount(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("TransactionCount() error = %v", err)
	}
	wantBalances, err := repo.WalletBalancesForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom() error = %v", err)
	}

	replayWorker := setupReconcileWorker(t, repo)
	drainWorkerUntilEmpty(t, ctx, replayWorker)

	gotCount, err := repo.TransactionCount(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("TransactionCount() after replay error = %v", err)
	}
	if gotCount != wantCount {
		t.Errorf("TransactionCount(%s) after replay = %d, want unchanged %d", fx.roomID, gotCount, wantCount)
	}

	gotBalances, err := repo.WalletBalancesForRoom(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("WalletBalancesForRoom() after replay error = %v", err)
	}
	if !reflect.DeepEqual(gotBalances, wantBalances) {
		t.Errorf("WalletBalancesForRoom(%s) after replay = %+v, want unchanged %+v", fx.roomID, gotBalances, wantBalances)
	}

	poolBal, err := repo.PoolBalance(ctx, fx.roomID)
	if err != nil {
		t.Fatalf("PoolBalance() after replay error = %v", err)
	}
	if poolBal != 0 {
		t.Errorf("PoolBalance(%s) after replay = %d, want 0", fx.roomID, poolBal)
	}
}
