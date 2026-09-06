package room

import (
	"bytes"
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

// sequenceReader returns each byte slice in sequence, then repeats the
// last one forever — used to drive GenerateCode deterministically.
type sequenceReader struct {
	seqs [][]byte
	idx  int
}

func (r *sequenceReader) Read(p []byte) (int, error) {
	seq := r.seqs[r.idx]
	if r.idx < len(r.seqs)-1 {
		r.idx++
	}
	return copy(p, seq), nil
}

func TestCreate_CodeCollision(t *testing.T) {
	store := newTestStore(t)
	issuer := testIssuer(t)

	t.Run("collision is retried", func(t *testing.T) {
		svc := NewService(store, issuer)
		svc.rand = &sequenceReader{seqs: [][]byte{
			bytes.Repeat([]byte{0x00}, CodeLen),
			bytes.Repeat([]byte{0x01}, CodeLen),
		}}

		hostA := testID(t, "hostA")
		createdA, err := svc.Create(context.Background(), hostA, "A", 500)
		if err != nil {
			t.Fatalf("Create(A) = %v, want nil", err)
		}

		hostB := testID(t, "hostB")
		createdB, err := svc.Create(context.Background(), hostB, "B", 500)
		if err != nil {
			t.Fatalf("Create(B) = %v, want nil", err)
		}

		if createdA.Code == createdB.Code {
			t.Fatalf("Create(A).Code == Create(B).Code == %q, want distinct codes", createdA.Code)
		}

		gotA, err := store.RoomByCode(context.Background(), createdA.Code)
		if err != nil || gotA != createdA.RoomID {
			t.Errorf("RoomByCode(A) = (%q, %v), want (%q, nil)", gotA, err, createdA.RoomID)
		}
		gotB, err := store.RoomByCode(context.Background(), createdB.Code)
		if err != nil || gotB != createdB.RoomID {
			t.Errorf("RoomByCode(B) = (%q, %v), want (%q, nil)", gotB, err, createdB.RoomID)
		}
	})

	t.Run("exhaustion", func(t *testing.T) {
		svc := NewService(store, issuer)
		// 0x02, not 0x00/0x01: those were already claimed by the
		// "collision is retried" subtest sharing this same store.
		svc.rand = &sequenceReader{seqs: [][]byte{bytes.Repeat([]byte{0x02}, CodeLen)}}

		host1 := testID(t, "host1")
		if _, err := svc.Create(context.Background(), host1, "First", 500); err != nil {
			t.Fatalf("Create(first) = %v, want nil", err)
		}

		host2 := testID(t, "host2")
		_, err := svc.Create(context.Background(), host2, "Second", 500)
		if !errors.Is(err, ErrCodeExhausted) {
			t.Fatalf("Create(second) err = %v, want ErrCodeExhausted", err)
		}
	})
}
