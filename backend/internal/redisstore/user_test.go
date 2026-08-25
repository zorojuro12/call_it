package redisstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func TestCreateUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := testID(t, "user")
	email := userID + "@example.com"

	u := User{
		ID:           userID,
		Email:        email,
		DisplayName:  "Alice",
		PasswordHash: "$argon2id$fake$hash",
		Balance:      domain.StartingBalance,
	}

	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser() = %v, want nil", err)
	}

	got, err := store.User(ctx, userID)
	if err != nil {
		t.Fatalf("User() = %v, want nil", err)
	}
	if got.ID != userID || got.Email != email || got.DisplayName != "Alice" ||
		got.PasswordHash != "$argon2id$fake$hash" || got.Balance != domain.StartingBalance {
		t.Errorf("User() = %+v, does not match written fields", got)
	}
	if got.CreatedAt.IsZero() || time.Since(got.CreatedAt) > 5*time.Second {
		t.Errorf("User().CreatedAt = %v, want non-zero and within 5s of now", got.CreatedAt)
	}

	byEmail, err := store.UserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("UserByEmail() = %v, want nil", err)
	}
	if byEmail.ID != userID {
		t.Errorf("UserByEmail().ID = %q, want %q", byEmail.ID, userID)
	}

	if _, err := store.User(ctx, "no-such-user"); !errors.Is(err, ErrNotFound) {
		t.Errorf("User(no-such-user) err = %v, want ErrNotFound", err)
	}
	if _, err := store.UserByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByEmail(nobody) err = %v, want ErrNotFound", err)
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	email := testID(t, "user") + "@example.com"

	aID := testID(t, "userA")
	if err := store.CreateUser(ctx, User{ID: aID, Email: email, DisplayName: "First", Balance: domain.StartingBalance}); err != nil {
		t.Fatalf("CreateUser(A) = %v, want nil", err)
	}

	bID := testID(t, "userB")
	err := store.CreateUser(ctx, User{ID: bID, Email: email, DisplayName: "Second", Balance: 42})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("CreateUser(B) err = %v, want ErrAlreadyExists", err)
	}

	byEmail, err := store.UserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("UserByEmail() = %v, want nil", err)
	}
	if byEmail.ID != aID || byEmail.DisplayName != "First" || byEmail.Balance != domain.StartingBalance {
		t.Errorf("UserByEmail() = %+v, want A's untouched fields", byEmail)
	}

	if _, err := store.User(ctx, bID); !errors.Is(err, ErrNotFound) {
		t.Errorf("User(B) err = %v, want ErrNotFound — B's hash must never have been written", err)
	}
}

func TestTopUpBalance(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	id1 := testID(t, "user")
	mustCreateUser(t, store, id1, 300)
	credited, newBalance, err := store.TopUpBalance(ctx, id1, 1000)
	if err != nil {
		t.Fatalf("TopUpBalance(300->1000) = %v, want nil", err)
	}
	if credited != 700 || newBalance != 1000 {
		t.Errorf("TopUpBalance(300->1000) = (%d, %d), want (700, 1000)", credited, newBalance)
	}
	got, err := store.User(ctx, id1)
	if err != nil {
		t.Fatalf("User() = %v, want nil", err)
	}
	if got.Balance != 1000 {
		t.Errorf("User().Balance = %d, want 1000", got.Balance)
	}

	id2 := testID(t, "user")
	mustCreateUser(t, store, id2, 0)
	credited2, newBalance2, err := store.TopUpBalance(ctx, id2, 1000)
	if err != nil {
		t.Fatalf("TopUpBalance(0->1000) = %v, want nil", err)
	}
	if credited2 != 1000 || newBalance2 != 1000 {
		t.Errorf("TopUpBalance(0->1000) = (%d, %d), want (1000, 1000)", credited2, newBalance2)
	}

	if _, _, err := store.TopUpBalance(ctx, "no-such-user", 1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("TopUpBalance(no-such-user) err = %v, want ErrNotFound", err)
	}
}

func mustCreateUser(t *testing.T, store *Store, userID string, balance domain.Tokens) {
	t.Helper()
	u := User{ID: userID, Email: userID + "@example.com", DisplayName: "Test", PasswordHash: "hash", Balance: balance}
	if err := store.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("mustCreateUser(%s): %v", userID, err)
	}
}
