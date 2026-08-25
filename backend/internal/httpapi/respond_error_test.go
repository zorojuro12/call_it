package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
)

func TestWriteError_Mapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"invalid email", auth.ErrInvalidEmail, 400, "validation_error"},
		{"weak password", auth.ErrWeakPassword, 400, "validation_error"},
		{"invalid display name", auth.ErrInvalidDisplayName, 400, "validation_error"},
		{"invalid buy-in", domain.ErrInvalidBuyIn, 400, "validation_error"},
		{"invalid token", auth.ErrInvalidToken, 401, "unauthorized"},
		{"token expired", auth.ErrTokenExpired, 401, "unauthorized"},
		{"invalid credentials", account.ErrInvalidCredentials, 401, "invalid_credentials"},
		{"not found", redisstore.ErrNotFound, 404, "not_found"},
		{"email taken", account.ErrEmailTaken, 409, "email_taken"},
		{"room not joinable", room.ErrNotJoinable, 409, "room_not_joinable"},
		{"refill not eligible", domain.ErrRefillNotEligible, 409, "refill_not_eligible"},
		{"refill quota exhausted", domain.ErrRefillQuotaExhausted, 429, "refill_quota_exhausted"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response body does not decode as JSON: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error.code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})

		t.Run(tt.name+" wrapped", func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", tt.err)
			rec := httptest.NewRecorder()
			WriteError(rec, wrapped)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response body does not decode as JSON: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("wrapped error.code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}
