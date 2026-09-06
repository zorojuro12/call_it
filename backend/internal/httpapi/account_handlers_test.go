package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestClaimRefill(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	email := testID(t, "refill") + "@example.com"
	regBody := `{"email":"` + email + `","password":"correct horse battery","display_name":"Alice"}`
	rec := registerRaw(mux, regBody)
	if rec.Code != 201 {
		t.Fatalf("register: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var regResp struct {
		Data struct {
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
			Token string `json:"token"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &regResp)
	setUserBalanceForTest(t, regResp.Data.Account.ID, 250)

	t.Run("eligible claim", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/accounts/me/refills", nil)
		req.Header.Set("Authorization", "Bearer "+regResp.Data.Token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != 201 {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				Credited  int64  `json:"credited"`
				Balance   int64  `json:"balance"`
				Remaining int    `json:"remaining"`
				ResetAt   string `json:"reset_at"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response does not decode: %v", err)
		}
		if resp.Data.Credited != 750 {
			t.Errorf("data.credited = %d, want 750", resp.Data.Credited)
		}
		if resp.Data.Balance != 1000 {
			t.Errorf("data.balance = %d, want 1000", resp.Data.Balance)
		}
		if resp.Data.Remaining != 2 {
			t.Errorf("data.remaining = %d, want 2", resp.Data.Remaining)
		}
		if resp.Data.ResetAt == "" {
			t.Error("data.reset_at is empty")
		}
		if rec.Header().Get("X-RateLimit-Limit") != "3" {
			t.Errorf("X-RateLimit-Limit = %q, want 3", rec.Header().Get("X-RateLimit-Limit"))
		}
		if rec.Header().Get("X-RateLimit-Remaining") != "2" {
			t.Errorf("X-RateLimit-Remaining = %q, want 2", rec.Header().Get("X-RateLimit-Remaining"))
		}
	})

	t.Run("without a token", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/accounts/me/refills", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		email2 := testID(t, "refill2") + "@example.com"
		regBody2 := `{"email":"` + email2 + `","password":"correct horse battery","display_name":"Bob"}`
		rec2 := registerRaw(mux, regBody2)
		var regResp2 struct {
			Data struct{ Token string } `json:"data"`
		}
		json.Unmarshal(rec2.Body.Bytes(), &regResp2)

		req := httptest.NewRequest("POST", "/api/v1/accounts/me/refills", strings.NewReader(`{"user_id":"someone-else"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+regResp2.Data.Token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400 (unknown field rejected), body: %s", rec.Code, rec.Body.String())
		}
	})
}

func claimRefillRaw(mux http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/accounts/me/refills", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestClaimRefill_Rejections(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	registerAt := func(t *testing.T, balance int64) (token, userID string) {
		t.Helper()
		email := testID(t, "rej") + "@example.com"
		regBody := `{"email":"` + email + `","password":"correct horse battery","display_name":"Alice"}`
		rec := registerRaw(mux, regBody)
		if rec.Code != 201 {
			t.Fatalf("register: status = %d, body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				Account struct {
					ID string `json:"id"`
				} `json:"account"`
				Token string `json:"token"`
			} `json:"data"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if balance != 1000 {
			setUserBalanceForTest(t, resp.Data.Account.ID, balance)
		}
		return resp.Data.Token, resp.Data.Account.ID
	}

	t.Run("already at target", func(t *testing.T) {
		token, userID := registerAt(t, 1000)
		rec := claimRefillRaw(mux, token)
		if rec.Code != 409 {
			t.Fatalf("status = %d, want 409, body: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Code != "refill_not_eligible" {
			t.Errorf("error.code = %q, want refill_not_eligible", body.Error.Code)
		}

		u, err := deps.Store.User(context.Background(), userID)
		if err != nil || u.Balance != 1000 {
			t.Errorf("balance = (%d, %v), want (1000, nil) — unchanged", u.Balance, err)
		}

		// The 409 must not have consumed quota: a genuine claim from a
		// lowered balance still succeeds afterward.
		setUserBalanceForTest(t, userID, 100)
		rec2 := claimRefillRaw(mux, token)
		if rec2.Code != 201 {
			t.Errorf("genuine claim after a 409: status = %d, want 201, body: %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("quota exhausted", func(t *testing.T) {
		token, userID := registerAt(t, 100)
		for i := 0; i < 3; i++ {
			rec := claimRefillRaw(mux, token)
			if rec.Code != 201 {
				t.Fatalf("claim %d: status = %d, want 201, body: %s", i+1, rec.Code, rec.Body.String())
			}
			setUserBalanceForTest(t, userID, 100)
		}

		rec := claimRefillRaw(mux, token)
		if rec.Code != 429 {
			t.Fatalf("4th claim: status = %d, want 429, body: %s", rec.Code, rec.Body.String())
		}
		retryAfter := rec.Header().Get("Retry-After")
		if n, err := strconv.Atoi(retryAfter); err != nil || n <= 0 {
			t.Errorf("Retry-After = %q, want a positive integer", retryAfter)
		}

		u, err := deps.Store.User(context.Background(), userID)
		if err != nil || u.Balance != 100 {
			t.Errorf("balance = (%d, %v), want (100, nil) — the refused claim must credit nothing", u.Balance, err)
		}
	})
}
