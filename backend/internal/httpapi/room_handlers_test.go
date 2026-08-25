package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
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

func createRoomForTest(t *testing.T, mux http.Handler, token string, buyIn int) (roomID, code string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/rooms", strings.NewReader(`{"buy_in":`+strconv.Itoa(buyIn)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create room: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			RoomID string `json:"room_id"`
			Code   string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("create room response does not decode: %v", err)
	}
	return resp.Data.RoomID, resp.Data.Code
}

func TestJoinRoom_Guest(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)
	hostToken := registerAndGetToken(t, mux, testID(t, "host")+"@example.com")
	roomID, code := createRoomForTest(t, mux, hostToken, 500)

	joinRaw := func(joinCode, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/rooms/"+joinCode+"/participants", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("valid join", func(t *testing.T) {
		rec := joinRaw(code, `{"display_name":"Bob"}`)
		if rec.Code != 201 {
			t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				RoomID         string `json:"room_id"`
				Guest          bool   `json:"guest"`
				SessionBalance int64  `json:"session_balance"`
				PartialBuyIn   bool   `json:"partial_buy_in"`
				Token          string `json:"token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response does not decode: %v", err)
		}
		if !resp.Data.Guest {
			t.Error("data.guest = false, want true")
		}
		if resp.Data.SessionBalance != 500 {
			t.Errorf("data.session_balance = %d, want 500", resp.Data.SessionBalance)
		}
		if resp.Data.PartialBuyIn {
			t.Error("data.partial_buy_in = true, want false")
		}
		if resp.Data.RoomID != roomID {
			t.Errorf("data.room_id = %q, want %q", resp.Data.RoomID, roomID)
		}

		claims, err := deps.Issuer.Verify(resp.Data.Token)
		if err != nil {
			t.Fatalf("token does not verify: %v", err)
		}
		if !claims.Guest || claims.DisplayName != "Bob" || claims.RoomID != roomID || claims.UserID == "" {
			t.Errorf("claims = %+v, want Guest=true DisplayName=Bob RoomID=%s non-empty UserID", claims, roomID)
		}

		balance, err := deps.Store.Balance(context.Background(), roomID, claims.UserID)
		if err != nil || balance != 500 {
			t.Errorf("store.Balance() = (%d, %v), want (500, nil)", balance, err)
		}
		count, err := deps.Store.PlayerCount(context.Background(), roomID)
		if err != nil || count != 1 {
			t.Errorf("store.PlayerCount() = (%d, %v), want (1, nil) — host excluded", count, err)
		}
	})

	t.Run("two guests get distinct user IDs", func(t *testing.T) {
		rec1 := joinRaw(code, `{"display_name":"Carol"}`)
		rec2 := joinRaw(code, `{"display_name":"Dave"}`)
		var r1, r2 struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		json.Unmarshal(rec1.Body.Bytes(), &r1)
		json.Unmarshal(rec2.Body.Bytes(), &r2)
		c1, _ := deps.Issuer.Verify(r1.Data.Token)
		c2, _ := deps.Issuer.Verify(r2.Data.Token)
		if c1.UserID == c2.UserID {
			t.Errorf("two guest joins produced the same UserID %q", c1.UserID)
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		rec := joinRaw("ZZZZZZ", `{"display_name":"Bob"}`)
		if rec.Code != 404 {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("missing display name", func(t *testing.T) {
		rec := joinRaw(code, `{}`)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("display name too long", func(t *testing.T) {
		rec := joinRaw(code, `{"display_name":"`+strings.Repeat("a", 33)+`"}`)
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("lowercase code for an uppercase room is not found", func(t *testing.T) {
		rec := joinRaw(strings.ToLower(code), `{"display_name":"Bob"}`)
		if rec.Code != 404 {
			t.Errorf("status = %d, want 404 — codes are case-sensitive", rec.Code)
		}
	})
}

type joinAccountResponse struct {
	Data struct {
		SessionBalance int64  `json:"session_balance"`
		PartialBuyIn   bool   `json:"partial_buy_in"`
		Guest          bool   `json:"guest"`
		Token          string `json:"token"`
	} `json:"data"`
}

func joinWithToken(t *testing.T, mux http.Handler, code, token string) (int, joinAccountResponse) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/rooms/"+code+"/participants", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp joinAccountResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

// registerWithBalance registers a fresh account (seeded to
// domain.StartingBalance=1000 by Register) then, when want differs, sets
// its balance directly through the store — TopUpBalance only raises a
// balance toward a target, so it can't lower one below 1000, and this
// test needs to reach 900 and 200 as well as 10000.
func registerWithBalance(t *testing.T, deps Deps, mux http.Handler, want int64) (token, userID string) {
	t.Helper()
	email := testID(t, "acct") + "@example.com"
	regBody := `{"email":"` + email + `","password":"correct horse battery","display_name":"AcctHolder"}`
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
	userID = resp.Data.Account.ID

	if want != 1000 {
		setUserBalanceForTest(t, userID, want)
	}
	return resp.Data.Token, userID
}

// setUserBalanceForTest and setSessionBalanceForTest write directly
// through a raw Redis client, bypassing the Store abstraction — there
// is no production method for this, deliberately, since only a real
// game session should ever move these balances.
func setUserBalanceForTest(t *testing.T, userID string, balance int64) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testDB})
	defer client.Close()
	if err := client.HSet(context.Background(), redisstore.UserKey(userID), "balance", strconv.FormatInt(balance, 10)).Err(); err != nil {
		t.Fatalf("setUserBalanceForTest(%s, %d): %v", userID, balance, err)
	}
}

func setSessionBalanceForTest(t *testing.T, roomID, userID string, balance int64) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testDB})
	defer client.Close()
	if err := client.HSet(context.Background(), redisstore.RoomWalletsKey(roomID), userID, strconv.FormatInt(balance, 10)).Err(); err != nil {
		t.Fatalf("setSessionBalanceForTest(%s, %s, %d): %v", roomID, userID, balance, err)
	}
}

func TestJoinRoom_Account(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)
	hostToken := registerAndGetToken(t, mux, testID(t, "host")+"@example.com")
	roomID, code := createRoomForTest(t, mux, hostToken, 500)

	t.Run("balance 10000", func(t *testing.T) {
		token, userID := registerWithBalance(t, deps, mux, 10000)
		status, resp := joinWithToken(t, mux, code, token)
		if status != 201 {
			t.Fatalf("status = %d, want 201", status)
		}
		if resp.Data.SessionBalance != 1500 {
			t.Errorf("session_balance = %d, want 1500 (3x500)", resp.Data.SessionBalance)
		}
		if resp.Data.PartialBuyIn {
			t.Error("partial_buy_in = true, want false")
		}
		if resp.Data.Guest {
			t.Error("guest = true, want false")
		}

		u, err := deps.Store.User(context.Background(), userID)
		if err != nil || u.Balance != 10000 {
			t.Errorf("persistent balance = (%d, %v), want (10000, nil) — joining must not debit it", u.Balance, err)
		}

		claims, err := deps.Issuer.Verify(resp.Data.Token)
		if err != nil {
			t.Fatalf("token does not verify: %v", err)
		}
		if claims.DisplayName != "AcctHolder" {
			t.Errorf("claims.DisplayName = %q, want AcctHolder (from the stored account, not the body)", claims.DisplayName)
		}
	})

	t.Run("balance 900", func(t *testing.T) {
		token, _ := registerWithBalance(t, deps, mux, 900)
		status, resp := joinWithToken(t, mux, code, token)
		if status != 201 {
			t.Fatalf("status = %d, want 201", status)
		}
		if resp.Data.SessionBalance != 900 {
			t.Errorf("session_balance = %d, want 900", resp.Data.SessionBalance)
		}
		if resp.Data.PartialBuyIn {
			t.Error("partial_buy_in = true, want false")
		}
	})

	t.Run("balance 200", func(t *testing.T) {
		token, _ := registerWithBalance(t, deps, mux, 200)
		status, resp := joinWithToken(t, mux, code, token)
		if status != 201 {
			t.Fatalf("status = %d, want 201", status)
		}
		if resp.Data.SessionBalance != 200 {
			t.Errorf("session_balance = %d, want 200", resp.Data.SessionBalance)
		}
		if !resp.Data.PartialBuyIn {
			t.Error("partial_buy_in = false, want true")
		}
	})

	t.Run("rejoin surviving balance", func(t *testing.T) {
		token, userID := registerWithBalance(t, deps, mux, 10000)
		status, _ := joinWithToken(t, mux, code, token)
		if status != 201 {
			t.Fatalf("first join: status = %d, want 201", status)
		}
		setSessionBalanceForTest(t, roomID, userID, 300)
		status2, resp2 := joinWithToken(t, mux, code, token)
		if status2 != 201 {
			t.Fatalf("second join: status = %d, want 201", status2)
		}
		if resp2.Data.SessionBalance != 300 {
			t.Errorf("second join session_balance = %d, want 300 (surviving balance)", resp2.Data.SessionBalance)
		}
	})
}
