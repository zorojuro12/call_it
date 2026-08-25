package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/room"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	store := newTestStore(t)
	issuer := testIssuer(t)
	accounts := account.NewService(store, issuer)
	rooms := &room.Service{}
	return Deps{Accounts: accounts, Rooms: rooms, Store: store, Issuer: issuer}
}

func TestRegister(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	body := `{"email":"  Alice@Example.COM  ","password":"correct horse battery","display_name":"  Alice  "}`
	req := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Account struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Balance     int64  `json:"balance"`
			} `json:"account"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response body does not decode: %v (body: %s)", err, rec.Body.String())
	}

	if resp.Data.Account.ID == "" {
		t.Error("account.id is empty, want a non-empty UUID")
	}
	if resp.Data.Account.Email != "alice@example.com" {
		t.Errorf("account.email = %q, want alice@example.com", resp.Data.Account.Email)
	}
	if resp.Data.Account.DisplayName != "Alice" {
		t.Errorf("account.display_name = %q, want Alice", resp.Data.Account.DisplayName)
	}
	if resp.Data.Account.Balance != 1000 {
		t.Errorf("account.balance = %d, want 1000", resp.Data.Account.Balance)
	}

	claims, err := deps.Issuer.Verify(resp.Data.Token)
	if err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	if claims.UserID != resp.Data.Account.ID || claims.Guest || claims.RoomID != "" {
		t.Errorf("claims = %+v, want UserID=%s Guest=false RoomID=\"\"", claims, resp.Data.Account.ID)
	}

	raw := rec.Body.String()
	for _, s := range []string{"password", "correct horse", "argon2"} {
		if strings.Contains(raw, s) {
			t.Errorf("response body leaks %q: %s", s, raw)
		}
	}

	u, err := deps.Store.UserByEmail(req.Context(), "alice@example.com")
	if err != nil {
		t.Fatalf("store.UserByEmail() = %v, want nil", err)
	}
	if err := auth.VerifyPassword(u.PasswordHash, "correct horse battery"); err != nil {
		t.Errorf("stored password hash does not verify against the submitted password: %v", err)
	}
}
