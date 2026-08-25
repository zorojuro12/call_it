package httpapi

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// claimsContextKey is an unexported type so this package's context key
// can never collide with a key set by another package.
type claimsContextKey struct{}

// ClaimsFrom retrieves the verified claims a RequireAuth or OptionalAuth
// middleware attached to the request context.
func ClaimsFrom(ctx context.Context) (auth.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(auth.Claims)
	return claims, ok
}

const bearerPrefix = "Bearer "

// verifyBearer extracts and verifies the bearer token from r, if
// present. present reports whether an Authorization header existed at
// all — RequireAuth and OptionalAuth differ only in how they treat its
// absence.
func verifyBearer(issuer *auth.Issuer, r *http.Request) (claims auth.Claims, present bool, err error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return auth.Claims{}, false, nil
	}

	if !strings.HasPrefix(header, bearerPrefix) {
		return auth.Claims{}, true, auth.ErrInvalidToken
	}

	token := strings.TrimPrefix(header, bearerPrefix)
	claims, err = issuer.Verify(token)
	return claims, true, err
}

// RequireAuth rejects any request without a valid bearer token.
func RequireAuth(issuer *auth.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, present, err := verifyBearer(issuer, r)
			if !present {
				WriteError(w, auth.ErrInvalidToken)
				return
			}
			if err != nil {
				WriteError(w, err)
				return
			}
			ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// LimitPolicy parameterizes one RateLimit middleware instance: the
// limiter scope and window, and how to derive the per-request key.
type LimitPolicy struct {
	Scope  string
	Limit  int
	Window time.Duration
	KeyFn  func(*http.Request) string
}

// RateLimit throttles requests through the shared sliding-window
// limiter, annotating every response with X-RateLimit-* headers and
// denying over-limit requests with 429 and Retry-After. A store error
// is a 500 — a limiter that cannot reach Redis must fail closed, never
// wave traffic through.
func RateLimit(store *redisstore.Store, p LimitPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decision, err := store.Allow(r.Context(), p.Scope, p.KeyFn(r), p.Limit, p.Window)
			if err != nil {
				WriteError(w, err)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(p.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(decision.ResetAt.Unix(), 10))

			if !decision.Allowed {
				retryAfterSec := int(math.Ceil(decision.RetryAfter.Seconds()))
				if retryAfterSec < 1 {
					retryAfterSec = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
				WriteError(w, &APIError{Status: 429, Code: "rate_limit_exceeded", Message: "rate limit exceeded"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// authThrottle limits unauthenticated auth endpoints (register, login)
// by client IP — there is no user identity yet to key on.
func authThrottle(store *redisstore.Store) func(http.Handler) http.Handler {
	return RateLimit(store, LimitPolicy{
		Scope:  "auth",
		Limit:  10,
		Window: time.Minute,
		KeyFn:  ClientIP,
	})
}

// apiThrottle limits authenticated endpoints by the caller's user ID.
// It must run after RequireAuth in the chain, so claims are already in
// the request context by the time KeyFn reads them.
func apiThrottle(store *redisstore.Store) func(http.Handler) http.Handler {
	return RateLimit(store, LimitPolicy{
		Scope:  "api",
		Limit:  60,
		Window: time.Minute,
		KeyFn: func(r *http.Request) string {
			claims, _ := ClaimsFrom(r.Context())
			return claims.UserID
		},
	})
}

// ClientIP reads the caller's address from r.RemoteAddr only.
// X-Forwarded-For is deliberately not consulted: it is caller-supplied,
// so trusting it here would let any client mint unlimited fresh
// rate-limit buckets by varying a header. When a real proxy sits in
// front of this service, the trusted-proxy configuration to parse it
// correctly gets designed then.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// OptionalAuth proceeds without claims when no Authorization header is
// present, but rejects a present-and-invalid one exactly like
// RequireAuth — only absence is optional. This is what lets the join
// endpoint serve both guests and account holders through one route
// without a bad token silently degrading into a guest join.
func OptionalAuth(issuer *auth.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, present, err := verifyBearer(issuer, r)
			if !present {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				WriteError(w, err)
				return
			}
			ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
