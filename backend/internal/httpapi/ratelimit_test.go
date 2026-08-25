package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestClientIP(t *testing.T) {
	cases := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		want          string
	}{
		{"ipv4 with port", "192.0.2.1:54321", "", "192.0.2.1"},
		{"ipv6 with port", "[2001:db8::1]:443", "", "2001:db8::1"},
		{"x-forwarded-for is ignored", "192.0.2.1:54321", "1.2.3.4", "192.0.2.1"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if got := ClientIP(req); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
