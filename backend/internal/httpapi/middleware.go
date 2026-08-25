package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/zorojuro12/call_it/backend/internal/auth"
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
