package room

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func testIssuer(t *testing.T) *auth.Issuer {
	t.Helper()
	issuer, err := auth.NewIssuer([]byte("01234567890123456789012345678901"), time.Hour)
	if err != nil {
		t.Fatalf("auth.NewIssuer() = %v, want nil", err)
	}
	return issuer
}

func TestCreate(t *testing.T) {
	store := newTestStore(t)
	issuer := testIssuer(t)
	svc := NewService(store, issuer)
	ctx := context.Background()
	hostID := testID(t, "host")

	created, err := svc.Create(ctx, hostID, "Host", 500)
	if err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if len(created.Code) != CodeLen {
		t.Errorf("Code length = %d, want %d", len(created.Code), CodeLen)
	}
	if created.RoomID == "" {
		t.Error("RoomID is empty, want a UUID")
	}
	if created.BuyIn != 500 {
		t.Errorf("BuyIn = %d, want 500", created.BuyIn)
	}

	claims, err := issuer.Verify(created.Token)
	if err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	if claims.RoomID != created.RoomID || claims.Guest {
		t.Errorf("claims = %+v, want RoomID=%s Guest=false", claims, created.RoomID)
	}

	gotID, err := store.RoomByCode(ctx, created.Code)
	if err != nil {
		t.Fatalf("store.RoomByCode() = %v, want nil", err)
	}
	if gotID != created.RoomID {
		t.Errorf("store.RoomByCode() = %q, want %q", gotID, created.RoomID)
	}

	balance, err := store.Balance(ctx, created.RoomID, hostID)
	if err != nil {
		t.Fatalf("store.Balance() = %v, want nil", err)
	}
	if balance != 500 {
		t.Errorf("store.Balance() = %d, want 500 — the host must have a wallet", balance)
	}

	count, err := store.PlayerCount(ctx, created.RoomID)
	if err != nil {
		t.Fatalf("store.PlayerCount() = %v, want nil", err)
	}
	if count != 0 {
		t.Errorf("store.PlayerCount() = %d, want 0 — the host is excluded from the denominator", count)
	}

	if _, err := svc.Create(ctx, hostID, "Host", 50); !errors.Is(err, domain.ErrInvalidBuyIn) {
		t.Errorf("Create() with buy-in 50: err = %v, want ErrInvalidBuyIn", err)
	}
	if _, err := svc.Create(ctx, hostID, "Host", 20000); !errors.Is(err, domain.ErrInvalidBuyIn) {
		t.Errorf("Create() with buy-in 20000: err = %v, want ErrInvalidBuyIn", err)
	}
}
