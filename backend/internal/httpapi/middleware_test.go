package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/auth"
)

func testIssuer(t *testing.T) *auth.Issuer {
	t.Helper()
	issuer, err := auth.NewIssuer([]byte("01234567890123456789012345678901"), time.Hour)
	if err != nil {
		t.Fatalf("auth.NewIssuer() = %v, want nil", err)
	}
	return issuer
}

func probeHandler(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ran = true
		claims, ok := ClaimsFrom(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			OK     bool        `json:"ok"`
			Claims auth.Claims `json:"claims"`
		}{ok, claims})
	})
}

func TestRequireAuth(t *testing.T) {
	issuer := testIssuer(t)
	validToken, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Alice"})
	if err != nil {
		t.Fatalf("Issue() = %v, want nil", err)
	}

	cases := []struct {
		name       string
		header     string
		wantStatus int
		wantRan    bool
	}{
		{"valid bearer token", "Bearer " + validToken, 200, true},
		{"no header", "", 401, false},
		{"garbage token", "Bearer garbage", 401, false},
		{"missing bearer prefix", validToken, 401, false},
		{"basic auth", "Basic dXNlcjpwYXNz", 401, false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var ran bool
			mw := RequireAuth(issuer)(probeHandler(&ran))

			req := httptest.NewRequest("GET", "/probe", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ran != tt.wantRan {
				t.Errorf("probe handler ran = %v, want %v", ran, tt.wantRan)
			}
			if tt.wantStatus == 401 {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				json.Unmarshal(rec.Body.Bytes(), &body)
				if body.Error.Code != "unauthorized" {
					t.Errorf("error.code = %q, want unauthorized", body.Error.Code)
				}
			}
		})
	}

	t.Run("valid token exposes claims to the handler", func(t *testing.T) {
		var ran bool
		mw := RequireAuth(issuer)(probeHandler(&ran))
		req := httptest.NewRequest("GET", "/probe", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)

		var body struct {
			OK     bool        `json:"ok"`
			Claims auth.Claims `json:"claims"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response body does not decode: %v", err)
		}
		if !body.OK {
			t.Error("ClaimsFrom ok = false, want true")
		}
		if body.Claims.UserID != "u1" || body.Claims.DisplayName != "Alice" {
			t.Errorf("claims = %+v, want UserID=u1 DisplayName=Alice", body.Claims)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		// auth.Issuer's clock seam is unexported and package-private to
		// internal/auth's own tests; here a 1ns TTL plus a short real
		// sleep produces a genuinely expired token deterministically
		// enough for this middleware-layer test.
		shortLived, err := auth.NewIssuer([]byte("01234567890123456789012345678901"), time.Nanosecond)
		if err != nil {
			t.Fatalf("NewIssuer() = %v, want nil", err)
		}
		tok, err := shortLived.Issue(auth.Claims{UserID: "u1"})
		if err != nil {
			t.Fatalf("Issue() = %v, want nil", err)
		}
		time.Sleep(10 * time.Millisecond)

		var ran bool
		mw := RequireAuth(shortLived)(probeHandler(&ran))
		req := httptest.NewRequest("GET", "/probe", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if ran {
			t.Error("probe handler ran, want it not to")
		}
	})
}

func TestOptionalAuth(t *testing.T) {
	issuer := testIssuer(t)
	validToken, err := issuer.Issue(auth.Claims{UserID: "u1"})
	if err != nil {
		t.Fatalf("Issue() = %v, want nil", err)
	}

	t.Run("no header proceeds without claims", func(t *testing.T) {
		var ran bool
		mw := OptionalAuth(issuer)(probeHandler(&ran))
		req := httptest.NewRequest("GET", "/probe", nil)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)

		if rec.Code != 200 || !ran {
			t.Fatalf("status = %d ran = %v, want 200 true", rec.Code, ran)
		}
		var body struct {
			OK bool `json:"ok"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		if body.OK {
			t.Error("ClaimsFrom ok = true, want false when no header present")
		}
	})

	t.Run("garbage token is still rejected", func(t *testing.T) {
		var ran bool
		mw := OptionalAuth(issuer)(probeHandler(&ran))
		req := httptest.NewRequest("GET", "/probe", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401 — present-but-invalid is still an error", rec.Code)
		}
		if ran {
			t.Error("probe handler ran, want it not to")
		}
	})

	t.Run("valid token exposes claims", func(t *testing.T) {
		var ran bool
		mw := OptionalAuth(issuer)(probeHandler(&ran))
		req := httptest.NewRequest("GET", "/probe", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body struct {
			OK bool `json:"ok"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		if !body.OK {
			t.Error("ClaimsFrom ok = false, want true")
		}
	})
}
