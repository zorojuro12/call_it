package account

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// setBalanceForTest writes a user's balance field directly, bypassing
// the Store abstraction — simulates play having driven the balance
// somewhere between refill claims, without a production-code method
// that exists only for tests to call.
func setBalanceForTest(t *testing.T, userID string, balance domain.Tokens) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testDB})
	defer client.Close()
	if err := client.HSet(context.Background(), redisstore.UserKey(userID), "balance", strconv.FormatInt(int64(balance), 10)).Err(); err != nil {
		t.Fatalf("setBalanceForTest(%s, %d): %v", userID, balance, err)
	}
}

func mustCreateUser(t *testing.T, store *redisstore.Store, userID string, balance domain.Tokens) {
	t.Helper()
	u := redisstore.User{
		ID:           userID,
		Email:        userID + "@example.com",
		DisplayName:  "Test",
		PasswordHash: "hash",
		Balance:      balance,
	}
	if err := store.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("mustCreateUser(%s): %v", userID, err)
	}
}

func TestClaimRefill_Eligible(t *testing.T) {
	store := newTestStore(t)
	svc := NewService(store, nil)
	ctx := context.Background()
	userID := testID(t, "user")

	mustCreateUser(t, store, userID, 250)

	result, err := svc.ClaimRefill(ctx, userID)
	if err != nil {
		t.Fatalf("ClaimRefill() = %v, want nil", err)
	}
	if result.Credited != 750 || result.Balance != 1000 || result.Remaining != 2 {
		t.Errorf("ClaimRefill() = %+v, want Credited=750 Balance=1000 Remaining=2", result)
	}
	if result.ResetAt.Before(time.Now()) || result.ResetAt.After(time.Now().Add(7*24*time.Hour)) {
		t.Errorf("ClaimRefill().ResetAt = %v, want within 7 days and in the future", result.ResetAt)
	}

	got, err := store.User(ctx, userID)
	if err != nil {
		t.Fatalf("store.User() = %v, want nil", err)
	}
	if got.Balance != 1000 {
		t.Errorf("store.User().Balance = %d, want 1000", got.Balance)
	}
}

func TestClaimRefill_QuotaExhausted(t *testing.T) {
	store := newTestStore(t)
	svc := NewService(store, nil)
	ctx := context.Background()
	userID := testID(t, "user")

	mustCreateUser(t, store, userID, 100)

	for i := 0; i < 3; i++ {
		if _, err := svc.ClaimRefill(ctx, userID); err != nil {
			t.Fatalf("ClaimRefill() claim %d = %v, want nil", i+1, err)
		}
		setBalanceForTest(t, userID, 100)
	}

	_, err := svc.ClaimRefill(ctx, userID)
	if !errors.Is(err, domain.ErrRefillQuotaExhausted) {
		t.Fatalf("ClaimRefill() 4th err = %v, want ErrRefillQuotaExhausted", err)
	}

	got, err := store.User(ctx, userID)
	if err != nil {
		t.Fatalf("store.User() = %v, want nil", err)
	}
	if got.Balance != 100 {
		t.Errorf("store.User().Balance = %d, want 100 — the refused claim must credit nothing", got.Balance)
	}
}
