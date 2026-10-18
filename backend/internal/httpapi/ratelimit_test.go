package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRateLimit(t *testing.T) {
	store := newTestStore(t)
	id := testID(t, "test")
	policy := LimitPolicy{
		Scope:  "test",
		Limit:  2,
		Window: time.Minute,
		KeyFn:  func(r *http.Request) string { return id },
	}

	var ran bool
	mw := RateLimit(store, policy)(probeHandler(&ran))

	req1 := httptest.NewRequest("GET", "/probe", nil)
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("1st: status = %d, want 200", rec1.Code)
	}
	if rec1.Header().Get("X-RateLimit-Limit") != "2" {
		t.Errorf("1st: X-RateLimit-Limit = %q, want 2", rec1.Header().Get("X-RateLimit-Limit"))
	}
	if rec1.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Errorf("1st: X-RateLimit-Remaining = %q, want 1", rec1.Header().Get("X-RateLimit-Remaining"))
	}
	resetHeader := rec1.Header().Get("X-RateLimit-Reset")
	resetUnix, err := strconv.ParseInt(resetHeader, 10, 64)
	if err != nil || resetUnix <= time.Now().Unix() {
		t.Errorf("1st: X-RateLimit-Reset = %q, want a future Unix second", resetHeader)
	}

	req2 := httptest.NewRequest("GET", "/probe", nil)
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("2nd: status = %d, want 200", rec2.Code)
	}
	if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("2nd: X-RateLimit-Remaining = %q, want 0", rec2.Header().Get("X-RateLimit-Remaining"))
	}

	ran = false
	req3 := httptest.NewRequest("GET", "/probe", nil)
	rec3 := httptest.NewRecorder()
	mw.ServeHTTP(rec3, req3)
	if rec3.Code != 429 {
		t.Fatalf("3rd: status = %d, want 429", rec3.Code)
	}
	if ran {
		t.Error("3rd: probe handler ran, want it not to")
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body does not decode as JSON: %v", err)
	}
	if body.Error.Code != "rate_limit_exceeded" {
		t.Errorf("3rd: error.code = %q, want rate_limit_exceeded", body.Error.Code)
	}
	retryAfter := rec3.Header().Get("Retry-After")
	if n, err := strconv.Atoi(retryAfter); err != nil || n <= 0 {
		t.Errorf("3rd: Retry-After = %q, want a positive integer", retryAfter)
	}

	otherPolicy := LimitPolicy{
		Scope:  "test",
		Limit:  2,
		Window: time.Minute,
		KeyFn:  func(r *http.Request) string { return "other" },
	}
	mwOther := RateLimit(store, otherPolicy)(probeHandler(&ran))
	reqOther := httptest.NewRequest("GET", "/probe", nil)
	recOther := httptest.NewRecorder()
	mwOther.ServeHTTP(recOther, reqOther)
	if recOther.Code != 200 {
		t.Errorf("other key: status = %d, want 200 — independent bucket from the exhausted one", recOther.Code)
	}
}

// TestThrottleTrustedProxy pins the deployment case: every request
// reaches the API from the same reverse proxy, so the auth and socket
// throttles must bucket by the proxy-reported client, or the whole site
// shares one bucket.
func TestThrottleTrustedProxy(t *testing.T) {
	// Arrange
	d := testDeps(t)
	d.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	mux := NewMux(d)

	viaProxy := func(method, path, body, client string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:5555"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	badLogin := `{"email":"nobody@example.invalid","password":"wrong-password-123"}`

	t.Run("auth scope buckets by forwarded client", func(t *testing.T) {
		// Act: exhaust client A's bucket
		for i := 1; i <= 10; i++ {
			if code := viaProxy("POST", "/api/v1/auth/login", badLogin, "203.0.113.1"); code != 401 {
				t.Fatalf("client A attempt %d: status = %d, want 401", i, code)
			}
		}

		// Assert
		if code := viaProxy("POST", "/api/v1/auth/login", badLogin, "203.0.113.1"); code != 429 {
			t.Fatalf("client A attempt 11: status = %d, want 429", code)
		}
		if code := viaProxy("POST", "/api/v1/auth/login", badLogin, "203.0.113.2"); code != 401 {
			t.Errorf("client B behind the same proxy: status = %d, want 401 — its own bucket, not A's", code)
		}
	})

	t.Run("socket scope buckets unauthenticated attempts by forwarded client", func(t *testing.T) {
		// Act: exhaust client C's bucket
		for i := 1; i <= wsConnectLimit; i++ {
			if code := viaProxy("GET", "/api/v1/socket", "", "203.0.113.3"); code == 429 {
				t.Fatalf("client C attempt %d: status = 429 before the limit of %d", i, wsConnectLimit)
			}
		}

		// Assert
		if code := viaProxy("GET", "/api/v1/socket", "", "203.0.113.3"); code != 429 {
			t.Fatalf("client C attempt %d: status = %d, want 429", wsConnectLimit+1, code)
		}
		if code := viaProxy("GET", "/api/v1/socket", "", "203.0.113.4"); code == 429 {
			t.Errorf("client D behind the same proxy: status = 429, want its own bucket")
		}
	})
}
