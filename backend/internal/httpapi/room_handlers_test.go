package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func registerAndGetToken(t *testing.T, mux http.Handler, email string) string {
	t.Helper()
	body := `{"email":"` + email + `","password":"correct horse battery","display_name":"Host"}`
	rec := registerRaw(mux, body)
	if rec.Code != 201 {
		t.Fatalf("register: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("register response does not decode: %v", err)
	}
	return resp.Data.Token
}

func TestCreateRoom(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	token := registerAndGetToken(t, mux, testID(t, "host")+"@example.com")

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{"buy_in":500}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != 201 {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				Code   string `json:"code"`
				RoomID string `json:"room_id"`
				BuyIn  int64  `json:"buy_in"`
				Token  string `json:"token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response does not decode: %v", err)
		}
		if len(resp.Data.Code) != 6 {
			t.Errorf("data.code length = %d, want 6", len(resp.Data.Code))
		}
		if resp.Data.RoomID == "" {
			t.Error("data.room_id is empty")
		}
		if resp.Data.BuyIn != 500 {
			t.Errorf("data.buy_in = %d, want 500", resp.Data.BuyIn)
		}
		claims, err := deps.Issuer.Verify(resp.Data.Token)
		if err != nil {
			t.Fatalf("token does not verify: %v", err)
		}
		if claims.RoomID != resp.Data.RoomID {
			t.Errorf("claims.RoomID = %q, want %q", claims.RoomID, resp.Data.RoomID)
		}
	})

	t.Run("without a token", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{"buy_in":500}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("buy-in too low", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{"buy_in":50}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("buy-in too high", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{"buy_in":20000}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing buy_in", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}
