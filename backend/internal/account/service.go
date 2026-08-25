// Package account orchestrates registration, login, and refill claims
// against internal/redisstore and internal/auth's pure primitives. It is
// what internal/auth intentionally is not: the I/O-bearing layer, kept
// separate so internal/auth stays unit-testable with nothing running.
package account

import (
	"context"
	"fmt"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// RefillScope and RefillWindow parameterize the shared rate limiter for
// the refill quota — never inline literals at the call site, so the
// policy has one definition.
const (
	RefillScope  = "refill"
	RefillWindow = 7 * 24 * time.Hour
)

// Service orchestrates account lifecycle operations.
type Service struct {
	store  *redisstore.Store
	issuer *auth.Issuer
}

// NewService constructs a Service bound to store and issuer.
func NewService(store *redisstore.Store, issuer *auth.Issuer) *Service {
	return &Service{store: store, issuer: issuer}
}

// RefillResult is what a successful ClaimRefill reports back.
type RefillResult struct {
	Credited  domain.Tokens
	Balance   domain.Tokens
	Remaining int
	ResetAt   time.Time
}

// ClaimRefill tops an eligible account's balance up to
// domain.RefillTarget, spending one of its rolling seven-day quota
// slots. The order — check eligibility, then spend quota, then credit —
// is what keeps a doomed request from consuming a slot it never used;
// eligibility runs before the limiter so a request already ineligible
// on balance alone never costs quota at all.
func (s *Service) ClaimRefill(ctx context.Context, userID string) (RefillResult, error) {
	u, err := s.store.User(ctx, userID)
	if err != nil {
		return RefillResult{}, fmt.Errorf("account: claim refill: %w", err)
	}

	if err := domain.CanRefill(u.Balance, 0); err != nil {
		return RefillResult{}, err
	}

	decision, err := s.store.Allow(ctx, RefillScope, userID, domain.RefillQuota, RefillWindow)
	if err != nil {
		return RefillResult{}, fmt.Errorf("account: claim refill: %w", err)
	}
	if !decision.Allowed {
		return RefillResult{}, domain.ErrRefillQuotaExhausted
	}

	credited, newBalance, err := s.store.TopUpBalance(ctx, userID, domain.RefillTarget)
	if err != nil {
		return RefillResult{}, fmt.Errorf("account: claim refill: %w", err)
	}

	return RefillResult{
		Credited:  credited,
		Balance:   newBalance,
		Remaining: decision.Remaining,
		ResetAt:   decision.ResetAt,
	}, nil
}
