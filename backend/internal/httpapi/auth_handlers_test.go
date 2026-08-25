package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	store := newTestStore(t)
	issuer := testIssuer(t)
	accounts := account.NewService(store, issuer)
	rooms := room.NewService(store, issuer)
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

func registerRaw(mux http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRegister_Rejections(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	dupEmail := testID(t, "dup") + "@example.com"
	dupBody := `{"email":"` + dupEmail + `","password":"correct horse battery","display_name":"First"}`
	if rec := registerRaw(mux, dupBody); rec.Code != 201 {
		t.Fatalf("first registration: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			"duplicate email",
			`{"email":"` + dupEmail + `","password":"correct horse battery","display_name":"Second"}`,
			409, "email_taken",
		},
		{
			"duplicate email different case and spacing",
			`{"email":"  ` + strings.ToUpper(dupEmail) + ` ","password":"correct horse battery","display_name":"Third"}`,
			409, "email_taken",
		},
		{
			"invalid email",
			`{"email":"nope","password":"correct horse battery","display_name":"A"}`,
			400, "validation_error",
		},
		{
			"short password",
			`{"email":"a@b.co","password":"short","display_name":"A"}`,
			400, "validation_error",
		},
		{
			"empty display name",
			`{"email":"a@b.co","password":"correct horse battery","display_name":""}`,
			400, "validation_error",
		},
		{
			"not json at all",
			`not json at all`,
			400, "validation_error",
		},
		{
			"unknown field rejected",
			`{"email":"a@b.co","password":"correct horse battery","display_name":"A","admin":true}`,
			400, "validation_error",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := registerRaw(mux, tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response body does not decode: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error.code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}

	// None of the rejected cases created an account: the dup-email
	// index still resolves to the original ("First"), and the
	// well-formed-but-otherwise-rejected "a@b.co" cases never claimed it.
	// UserByEmail takes an already-normalized address, same as the
	// registration handler normalizes on the way in — testID's mixed
	// case (from t.Name()) must be lowercased here too.
	byDup, err := deps.Store.UserByEmail(context.Background(), strings.ToLower(dupEmail))
	if err != nil {
		t.Fatalf("store.UserByEmail(dup) = %v, want nil", err)
	}
	if byDup.DisplayName != "First" {
		t.Errorf("store.UserByEmail(dup).DisplayName = %q, want First (unchanged)", byDup.DisplayName)
	}
	if _, err := deps.Store.UserByEmail(context.Background(), "a@b.co"); !errors.Is(err, redisstore.ErrNotFound) {
		t.Errorf("store.UserByEmail(a@b.co) err = %v, want ErrNotFound — no rejected case should have created it", err)
	}
}

func TestLogin_Success(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	email := testID(t, "login") + "@example.com"
	regBody := `{"email":"` + email + `","password":"correct horse battery","display_name":"Alice"}`
	if rec := registerRaw(mux, regBody); rec.Code != 201 {
		t.Fatalf("register: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	loginAndVerify := func(t *testing.T, loginEmail string) {
		t.Helper()
		body := `{"email":"` + loginEmail + `","password":"correct horse battery"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data struct {
				Account struct {
					Balance int64 `json:"balance"`
				} `json:"account"`
				Token string `json:"token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response body does not decode: %v", err)
		}
		if resp.Data.Account.Balance != 1000 {
			t.Errorf("account.balance = %d, want 1000", resp.Data.Account.Balance)
		}
		claims, err := deps.Issuer.Verify(resp.Data.Token)
		if err != nil {
			t.Fatalf("token does not verify: %v", err)
		}
		if claims.UserID == "" {
			t.Error("claims.UserID is empty")
		}

		raw := rec.Body.String()
		for _, s := range []string{"password", "correct horse"} {
			if strings.Contains(raw, s) {
				t.Errorf("response body leaks %q: %s", s, raw)
			}
		}
	}

	t.Run("normalized email", func(t *testing.T) {
		loginAndVerify(t, email)
	})
	t.Run("differently-cased and spaced email", func(t *testing.T) {
		loginAndVerify(t, "  "+strings.ToUpper(email)+" ")
	})
}

func TestLogin_NoEnumeration(t *testing.T) {
	deps := testDeps(t)
	mux := NewMux(deps)

	email := testID(t, "noenum") + "@example.com"
	regBody := `{"email":"` + email + `","password":"correct horse battery","display_name":"Alice"}`
	if rec := registerRaw(mux, regBody); rec.Code != 201 {
		t.Fatalf("register: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	loginRaw := func(loginEmail, password string) *httptest.ResponseRecorder {
		body := `{"email":"` + loginEmail + `","password":"` + password + `"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	wrongPassword := loginRaw(email, "totally wrong password")
	unknownEmail := loginRaw(testID(t, "unknown")+"@example.com", "correct horse battery")

	// Malformed-hash account: write directly through the store so a
	// corrupted record is exercised, not just a wrong guess.
	malformedID := testID(t, "malformed")
	malformedEmail := strings.ToLower(malformedID) + "@example.com"
	if err := deps.Store.CreateUser(context.Background(), redisstore.User{
		ID: malformedID, Email: malformedEmail, DisplayName: "Bad", PasswordHash: "garbage", Balance: 1000,
	}); err != nil {
		t.Fatalf("CreateUser(malformed) = %v, want nil", err)
	}
	malformedHash := loginRaw(malformedEmail, "anything")

	for _, rec := range []*httptest.ResponseRecorder{wrongPassword, unknownEmail, malformedHash} {
		if rec.Code != 401 {
			t.Errorf("status = %d, want 401, body: %s", rec.Code, rec.Body.String())
		}
	}

	normalize := func(rec *httptest.ResponseRecorder) string {
		var v any
		json.Unmarshal(rec.Body.Bytes(), &v)
		out, _ := json.Marshal(v)
		return string(out)
	}

	wpBody := normalize(wrongPassword)
	ueBody := normalize(unknownEmail)
	mhBody := normalize(malformedHash)

	if wpBody != ueBody {
		t.Errorf("wrong-password body %q != unknown-email body %q — response distinguishes them", wpBody, ueBody)
	}
	if wpBody != mhBody {
		t.Errorf("wrong-password body %q != malformed-hash body %q — response distinguishes them", wpBody, mhBody)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(wrongPassword.Body.Bytes(), &body)
	if body.Error.Code != "invalid_credentials" {
		t.Errorf("error.code = %q, want invalid_credentials", body.Error.Code)
	}
}
