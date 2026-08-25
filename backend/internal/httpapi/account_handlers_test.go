package httpapi

import (
	"encoding/json"
	"net/http/httptest"
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
