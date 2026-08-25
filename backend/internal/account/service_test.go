package account

import (
	"context"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

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
