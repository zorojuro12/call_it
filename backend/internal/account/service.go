// Package account orchestrates registration, login, and refill claims
// against internal/redisstore and internal/auth's pure primitives. It is
// what internal/auth intentionally is not: the I/O-bearing layer, kept
// separate so internal/auth stays unit-testable with nothing running.
package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// ErrEmailTaken is returned by Register when the email is already
// claimed by another account.
var ErrEmailTaken = errors.New("account: email is already registered")

// ErrInvalidCredentials is returned by Login for both an unknown email
// and a wrong password — the two must be indistinguishable, so no
// caller can tell which one it was from the error alone (Task 9 CP4).
var ErrInvalidCredentials = errors.New("account: email or password is incorrect")

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

// Account is the caller-facing projection of a registered user —
// deliberately narrower than redisstore.User, which also carries the
// password hash.
type Account struct {
	ID          string
	Email       string
	DisplayName string
	Balance     domain.Tokens
}

// Register creates a funded account and issues an account-scoped token.
// Validation runs before hashing, so a request that cannot succeed never
// pays argon2id's cost — that ordering also keeps registration from
// being a cheap CPU-exhaustion lever.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (Account, string, error) {
	normalizedEmail := auth.NormalizeEmail(email)
	if err := auth.ValidateEmail(normalizedEmail); err != nil {
		return Account{}, "", err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return Account{}, "", err
	}
	normalizedName := auth.NormalizeDisplayName(displayName)
	if err := auth.ValidateDisplayName(normalizedName); err != nil {
		return Account{}, "", err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return Account{}, "", fmt.Errorf("account: register: %w", err)
	}

	userID := uuid.NewString()
	u := redisstore.User{
		ID:           userID,
		Email:        normalizedEmail,
		DisplayName:  normalizedName,
		PasswordHash: hash,
		Balance:      domain.StartingBalance,
	}
	if err := s.store.CreateUser(ctx, u); err != nil {
		if errors.Is(err, redisstore.ErrAlreadyExists) {
			return Account{}, "", fmt.Errorf("%w: %s", ErrEmailTaken, normalizedEmail)
		}
		return Account{}, "", fmt.Errorf("account: register: %w", err)
	}

	token, err := s.issuer.Issue(auth.Claims{UserID: userID, DisplayName: normalizedName})
	if err != nil {
		return Account{}, "", fmt.Errorf("account: register: %w", err)
	}

	return Account{ID: userID, Email: normalizedEmail, DisplayName: normalizedName, Balance: domain.StartingBalance}, token, nil
}

// Login verifies credentials and issues an account-scoped token.
func (s *Service) Login(ctx context.Context, email, password string) (Account, string, error) {
	normalizedEmail := auth.NormalizeEmail(email)

	u, err := s.store.UserByEmail(ctx, normalizedEmail)
	if err != nil {
		return Account{}, "", fmt.Errorf("account: login: %w", err)
	}

	if err := auth.VerifyPassword(u.PasswordHash, password); err != nil {
		return Account{}, "", fmt.Errorf("account: login: %w", err)
	}

	token, err := s.issuer.Issue(auth.Claims{UserID: u.ID, DisplayName: u.DisplayName})
	if err != nil {
		return Account{}, "", fmt.Errorf("account: login: %w", err)
	}

	return Account{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Balance: u.Balance}, token, nil
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

	if credited == 0 {
		// The eligibility pre-check above used a snapshot read; a
		// concurrent claim can credit the balance to the target between
		// that read and this one. The request was ineligible after all —
		// it just couldn't be known until the atomic TopUpBalance ran.
		// Hand the slot back so a doomed request doesn't permanently
		// cost the caller a slot. A Revoke failure is ignored: the claim
		// genuinely credited nothing, and failing the request over it
		// would be a worse answer than a slightly conservative quota.
		_ = s.store.Revoke(ctx, RefillScope, userID, decision.Member)
		return RefillResult{}, domain.ErrRefillNotEligible
	}

	return RefillResult{
		Credited:  credited,
		Balance:   newBalance,
		Remaining: decision.Remaining,
		ResetAt:   decision.ResetAt,
	}, nil
}
