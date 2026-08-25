package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
)

// WriteData wraps v in the success envelope {"data": ...} and writes it
// with the given status.
func WriteData(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The header is already sent, so a failed encode can't change the
	// response status — log it rather than silently dropping it.
	if err := json.NewEncoder(w).Encode(struct {
		Data any `json:"data"`
	}{v}); err != nil {
		slog.Error("httpapi: failed to encode response", "error", err)
	}
}

// APIError is a status/code/message triple a handler can raise directly
// to bypass the mapping table for a one-off error.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

// errorTable maps a domain/store/auth sentinel to its HTTP status and
// error code, in the order checked. errors.Is walks the wrap chain, so
// every layer that wraps on the way up still maps correctly.
var errorTable = []struct {
	err    error
	status int
	code   string
}{
	{auth.ErrInvalidEmail, 400, "validation_error"},
	{auth.ErrWeakPassword, 400, "validation_error"},
	{auth.ErrInvalidDisplayName, 400, "validation_error"},
	{domain.ErrInvalidBuyIn, 400, "validation_error"},
	{auth.ErrInvalidToken, 401, "unauthorized"},
	{auth.ErrTokenExpired, 401, "unauthorized"},
	{account.ErrInvalidCredentials, 401, "invalid_credentials"},
	{redisstore.ErrNotFound, 404, "not_found"},
	{account.ErrEmailTaken, 409, "email_taken"},
	{room.ErrNotJoinable, 409, "room_not_joinable"},
	{domain.ErrRefillNotEligible, 409, "refill_not_eligible"},
	{domain.ErrRefillQuotaExhausted, 429, "refill_quota_exhausted"},
}

// apiErrorFor walks errorTable with errors.Is, in order, and returns the
// first match. A caller-supplied *APIError is used as-is.
func apiErrorFor(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}

	for _, row := range errorTable {
		if errors.Is(err, row.err) {
			return &APIError{Status: row.status, Code: row.code, Message: err.Error()}
		}
	}

	return &APIError{Status: 500, Code: "internal_error", Message: "an internal error occurred"}
}

// WriteError maps err to its documented status and code, then writes
// the error envelope {"error": {"code": ..., "message": ...}}.
func WriteError(w http.ResponseWriter, err error) {
	apiErr := apiErrorFor(err)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.Status)
	if encErr := json.NewEncoder(w).Encode(struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{apiErr.Code, apiErr.Message}}); encErr != nil {
		slog.Error("httpapi: failed to encode error response", "error", encErr)
	}
}
