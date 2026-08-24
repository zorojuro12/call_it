package redisstore

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// TestConcurrent_NoDoubleSpend proves that N goroutines racing a single
// wallet cannot spend more than the wallet holds — the property the
// whole atomic-Lua design exists to guarantee.
func TestConcurrent_NoDoubleSpend(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 1000})

	const goroutines = 100
	const stake = 50

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := store.PlaceWager(ctx, WagerRequest{
				RoomID: roomID, RoundID: roundID, UserID: "u1",
				Outcome: i % 3, Amount: stake, IdempotencyKey: fmt.Sprintf("dblspend-%s-%d", roundID, i),
			})
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	var accepted, rejected int
	for _, err := range errs {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, domain.ErrInsufficientFunds):
			rejected++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	if accepted != 20 {
		t.Errorf("accepted = %d, want 20 (1000/50)", accepted)
	}
	if rejected != 80 {
		t.Errorf("rejected = %d, want 80", rejected)
	}

	balance, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != 0 {
		t.Errorf("final balance = %d, want 0 (never negative)", balance)
	}

	_, total, err := store.Pools(ctx, roundID)
	if err != nil {
		t.Fatalf("Pools() = %v, want nil", err)
	}
	if total != 1000 {
		t.Errorf("pools total = %d, want 1000", total)
	}

	xlen, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox: %v", err)
	}
	if xlen != 20 {
		t.Errorf("XLEN outbox = %d, want 20 — one event per accepted wager", xlen)
	}
}

// TestConcurrent_TokenConservation proves that under mixed concurrent
// load across several players and outcomes, the sum of every wallet plus
// the pool total never drifts from what the room started with — the
// invariant that makes double-spend structurally impossible.
func TestConcurrent_TokenConservation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	players := map[string]domain.Tokens{"u1": 500, "u2": 500, "u3": 500, "u4": 500, "u5": 500}
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, players)

	const goroutines = 200
	rng := rand.New(rand.NewSource(42))
	userIDs := []string{"u1", "u2", "u3", "u4", "u5"}

	type job struct {
		userID  string
		outcome int
		amount  domain.Tokens
	}
	jobs := make([]job, goroutines)
	for i := range jobs {
		jobs[i] = job{
			userID:  userIDs[rng.Intn(len(userIDs))],
			outcome: rng.Intn(3),
			amount:  domain.Tokens(1 + rng.Intn(100)),
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	var successCount int
	var mu sync.Mutex

	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			<-start
			_, err := store.PlaceWager(ctx, WagerRequest{
				RoomID: roomID, RoundID: roundID, UserID: j.userID,
				Outcome: j.outcome, Amount: j.amount, IdempotencyKey: fmt.Sprintf("conserve-%s-%d", roundID, i),
			})
			switch {
			case err == nil:
				mu.Lock()
				successCount++
				mu.Unlock()
			case errors.Is(err, domain.ErrInsufficientFunds):
				// expected under contention
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i, j)
	}
	close(start)
	wg.Wait()

	var sumWallets int64
	for _, userID := range userIDs {
		balance, err := store.Balance(ctx, roomID, userID)
		if err != nil {
			t.Fatalf("Balance(%s) = %v, want nil", userID, err)
		}
		if balance < 0 {
			t.Errorf("balance(%s) = %d, want >= 0", userID, balance)
		}
		sumWallets += int64(balance)
	}

	pools, total, err := store.Pools(ctx, roundID)
	if err != nil {
		t.Fatalf("Pools() = %v, want nil", err)
	}
	var sumPools domain.Tokens
	for _, p := range pools {
		sumPools += p
	}
	if sumPools != total {
		t.Errorf("Σ pools (%d) != pools total (%d)", sumPools, total)
	}

	if sumWallets+int64(total) != 2500 {
		t.Errorf("Σ wallets(%d) + pools total(%d) = %d, want 2500", sumWallets, total, sumWallets+int64(total))
	}

	wagerFields, err := store.client.HGetAll(ctx, RoundWagersKey(roundID)).Result()
	if err != nil {
		t.Fatalf("HGETALL wagers: %v", err)
	}
	var sumWagers int64
	for _, v := range wagerFields {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("parse wager amount %q: %v", v, err)
		}
		sumWagers += n
	}
	if sumWagers != int64(total) {
		t.Errorf("Σ wagers(%d) != pools total(%d)", sumWagers, total)
	}

	xlen, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox: %v", err)
	}
	if xlen != int64(successCount) {
		t.Errorf("XLEN outbox = %d, want %d (number of successful calls)", xlen, successCount)
	}
}

// TestConcurrent_IdempotencyRace proves that 50 goroutines racing the
// byte-identical request — same idempotency key — debit the wallet
// exactly once. This is the strongest test of the idempotency design:
// the check and the write must be atomic with each other, or several
// goroutines could pass through the gap between a GET and a SET.
func TestConcurrent_IdempotencyRace(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lockAt := time.Now().Add(30 * time.Second)
	roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, map[string]domain.Tokens{"u1": 500})

	const goroutines = 50
	key := testID(t, "idem-race")
	req := WagerRequest{
		RoomID: roomID, RoundID: roundID, UserID: "u1",
		Outcome: 1, Amount: 200, IdempotencyKey: key,
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]WagerResult, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := store.PlaceWager(ctx, req)
			results[i] = res
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
	}
	first := results[0]
	for i, res := range results {
		if !reflect.DeepEqual(res, first) {
			t.Errorf("call %d result = %+v, want deep-equal to first %+v", i, res, first)
		}
	}

	balance, err := store.Balance(ctx, roomID, "u1")
	if err != nil {
		t.Fatalf("Balance() = %v, want nil", err)
	}
	if balance != 300 {
		t.Errorf("balance = %d, want 300 (debited exactly once)", balance)
	}

	wagerField, err := store.client.HGet(ctx, RoundWagersKey(roundID), WagerField("u1", 1)).Result()
	if err != nil {
		t.Fatalf("HGET wagers u1:1: %v", err)
	}
	if wagerField != "200" {
		t.Errorf("HGET wagers u1:1 = %q, want %q", wagerField, "200")
	}

	xlen, err := store.client.XLen(ctx, store.outboxStream).Result()
	if err != nil {
		t.Fatalf("XLEN outbox: %v", err)
	}
	if xlen != 1 {
		t.Errorf("XLEN outbox = %d, want 1", xlen)
	}
}

// TestFullRound_TokenConservation proves the conservation invariant Phase
// 1's fuzz test asserts in pure Go now holds across a real Redis round
// trip: concurrent wagers, a lock, and a settlement together.
func TestFullRound_TokenConservation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("resolves to a winning outcome", func(t *testing.T) {
		lockAt := time.Now().Add(30 * time.Second)
		players := map[string]domain.Tokens{"u1": 500, "u2": 500, "u3": 500, "u4": 500}
		roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, players)

		placeConcurrentWagers(t, store, roomID, roundID, players, 30)

		if err := store.LockRound(ctx, roundID); err != nil {
			t.Fatalf("LockRound() = %v, want nil", err)
		}

		winningOutcome, err := winningOutcomeWithNonEmptyPool(ctx, store, roundID)
		if err != nil {
			t.Fatalf("find winning outcome: %v", err)
		}

		settlement, err := store.SettleRound(ctx, roundID, winningOutcome, testID(t, "idem"))
		if err != nil {
			t.Fatalf("SettleRound() = %v, want nil", err)
		}

		var sumWallets, sumNet int64
		for userID := range players {
			balance, err := store.Balance(ctx, roomID, userID)
			if err != nil {
				t.Fatalf("Balance(%s) = %v, want nil", userID, err)
			}
			sumWallets += int64(balance)
		}
		for _, r := range settlement.Results {
			sumNet += int64(r.Net)
		}

		if sumWallets+int64(settlement.Dust) != 2000 {
			t.Errorf("Σ wallets(%d) + Dust(%d) = %d, want 2000", sumWallets, settlement.Dust, sumWallets+int64(settlement.Dust))
		}
		if settlement.Dust < 0 {
			t.Errorf("Dust = %d, want >= 0", settlement.Dust)
		}
		if sumNet != -int64(settlement.Dust) {
			t.Errorf("Σ Net(%d) != -Dust(%d)", sumNet, -int64(settlement.Dust))
		}

		round, err := store.Round(ctx, roundID)
		if err != nil {
			t.Fatalf("Round() = %v, want nil", err)
		}
		if round.Status != domain.RoundResolved {
			t.Errorf("status = %q, want %q", round.Status, domain.RoundResolved)
		}
	})

	t.Run("resolves to an empty outcome", func(t *testing.T) {
		lockAt := time.Now().Add(30 * time.Second)
		players := map[string]domain.Tokens{"u1": 500, "u2": 500, "u3": 500, "u4": 500}
		roomID, roundID := setupWagerRoom(t, store, "host1", 500, 3, lockAt, players)

		// Every wager on outcome 0 or 1 — outcome 2 stays empty.
		for i, userID := range []string{"u1", "u2", "u3", "u4"} {
			if _, err := store.PlaceWager(ctx, WagerRequest{
				RoomID: roomID, RoundID: roundID, UserID: userID,
				Outcome: i % 2, Amount: 100, IdempotencyKey: testID(t, "idem"),
			}); err != nil {
				t.Fatalf("PlaceWager(%s) = %v, want nil", userID, err)
			}
		}

		if err := store.LockRound(ctx, roundID); err != nil {
			t.Fatalf("LockRound() = %v, want nil", err)
		}

		settlement, err := store.SettleRound(ctx, roundID, 2, testID(t, "idem"))
		if err != nil {
			t.Fatalf("SettleRound() = %v, want nil", err)
		}
		if !settlement.Refunded {
			t.Errorf("Refunded = false, want true")
		}

		var sumWallets int64
		for userID := range players {
			balance, err := store.Balance(ctx, roomID, userID)
			if err != nil {
				t.Fatalf("Balance(%s) = %v, want nil", userID, err)
			}
			sumWallets += int64(balance)
		}
		if sumWallets != 2000 {
			t.Errorf("Σ wallets = %d, want 2000", sumWallets)
		}
		if settlement.Dust != 0 {
			t.Errorf("Dust = %d, want 0", settlement.Dust)
		}

		round, err := store.Round(ctx, roundID)
		if err != nil {
			t.Fatalf("Round() = %v, want nil", err)
		}
		if round.Status != domain.RoundRefunded {
			t.Errorf("status = %q, want %q", round.Status, domain.RoundRefunded)
		}
	})
}

// placeConcurrentWagers fires n concurrent wagers from a random player
// in players at a pseudo-random outcome and amount, ignoring individual
// wager errors (a losing race for funds is an expected outcome here,
// not a test failure) — the point is generating realistic concurrent
// pool state ahead of a lock/settle sequence.
func placeConcurrentWagers(t *testing.T, store *Store, roomID, roundID string, players map[string]domain.Tokens, n int) {
	t.Helper()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(7))
	userIDs := make([]string, 0, len(players))
	for userID := range players {
		userIDs = append(userIDs, userID)
	}

	// Precomputed sequentially — math/rand.Rand is not safe for
	// concurrent use, so the random draws must happen before any
	// goroutine starts, not inside one.
	type job struct {
		userID  string
		outcome int
		amount  domain.Tokens
	}
	jobs := make([]job, n)
	for i := range jobs {
		jobs[i] = job{
			userID:  userIDs[rng.Intn(len(userIDs))],
			outcome: rng.Intn(3),
			amount:  domain.Tokens(1 + rng.Intn(50)),
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			<-start
			_, _ = store.PlaceWager(ctx, WagerRequest{
				RoomID: roomID, RoundID: roundID, UserID: j.userID,
				Outcome: j.outcome, Amount: j.amount,
				IdempotencyKey: fmt.Sprintf("full-round-%s-%d", roundID, i),
			})
		}(i, j)
	}
	close(start)
	wg.Wait()
}

// winningOutcomeWithNonEmptyPool returns the first outcome index whose
// pool is non-empty, so settlement has a real winner to pay out.
func winningOutcomeWithNonEmptyPool(ctx context.Context, store *Store, roundID string) (int, error) {
	pools, _, err := store.Pools(ctx, roundID)
	if err != nil {
		return 0, err
	}
	for i, p := range pools {
		if p > 0 {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no outcome has a non-empty pool: %v", pools)
}
