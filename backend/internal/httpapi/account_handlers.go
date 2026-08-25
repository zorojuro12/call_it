package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/account"
)

// registerAccountRoutes wires the account routes onto mux.
func registerAccountRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("POST /api/v1/accounts/me/refills", RequireAuth(d.Issuer)(apiThrottle(d.Store)(http.HandlerFunc(handleClaimRefill(d)))))
}

type refillResponse struct {
	Credited  int64  `json:"credited"`
	Balance   int64  `json:"balance"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"reset_at"`
}

func handleClaimRefill(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := ClaimsFrom(r.Context())

		// The body carries no fields this endpoint reads — the user ID
		// comes only from the verified token — but a body naming an
		// unexpected field (e.g. a caller-supplied user_id) is still
		// rejected, so nothing is silently ignored.
		var empty struct{}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&empty); err != nil && !errors.Is(err, io.EOF) {
			WriteError(w, &APIError{Status: 400, Code: "validation_error", Message: "malformed request body"})
			return
		}

		result, err := d.Accounts.ClaimRefill(r.Context(), claims.UserID)
		if err != nil {
			var quotaErr *account.QuotaError
			if errors.As(err, &quotaErr) {
				retryAfterSec := int(math.Ceil(quotaErr.RetryAfter.Seconds()))
				if retryAfterSec < 1 {
					retryAfterSec = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
			}
			WriteError(w, err)
			return
		}

		w.Header().Set("X-RateLimit-Limit", "3")
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

		WriteData(w, 201, refillResponse{
			Credited:  int64(result.Credited),
			Balance:   int64(result.Balance),
			Remaining: result.Remaining,
			ResetAt:   result.ResetAt.Format(time.RFC3339),
		})
	}
}
