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
