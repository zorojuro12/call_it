package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/ws"
)

func TestWSRoute(t *testing.T) {
	// Arrange
	deps := testDeps(t)
	mux := NewMux(deps)
	server := httptest.NewServer(mux)
	defer server.Close()

	token, err := deps.Issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	// Act
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/api/v1/socket?token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)

	// Assert
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	defer conn.Close()
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage error: %v", err)
	}
	env, err := ws.Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if env.Type != ws.TypeConnected {
		t.Fatalf("Type = %q, want %q", env.Type, ws.TypeConnected)
	}

	// Act: dial without a token
	_, resp2, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/api/v1/socket", nil)

	// Assert
	if err == nil {
		t.Fatal("Dial without token succeeded, want failure")
	}
	if resp2 == nil || resp2.StatusCode != 401 {
		status := -1
		if resp2 != nil {
			status = resp2.StatusCode
		}
		t.Fatalf("status = %d, want 401", status)
	}
}

func TestWSThrottleKey(t *testing.T) {
	// Arrange
	issuer := testIssuer(t)
	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	keyFn := wsThrottleKey(issuer)

	t.Run("valid token keys by user ID", func(t *testing.T) {
		// Act
		req := httptest.NewRequest("GET", "/api/v1/socket?token="+token, nil)

		// Assert
		if got, want := keyFn(req), "user:u1"; got != want {
			t.Errorf("keyFn() = %q, want %q", got, want)
		}
	})

	t.Run("missing token falls back to IP", func(t *testing.T) {
		// Act
		req := httptest.NewRequest("GET", "/api/v1/socket", nil)
		req.RemoteAddr = "192.0.2.7:54321"

		// Assert
		if got, want := keyFn(req), "ip:192.0.2.7"; got != want {
			t.Errorf("keyFn() = %q, want %q", got, want)
		}
	})

	t.Run("garbage token falls back to IP", func(t *testing.T) {
		// Act
		req := httptest.NewRequest("GET", "/api/v1/socket?token=not-a-jwt", nil)
		req.RemoteAddr = "192.0.2.8:54321"

		// Assert
		if got, want := keyFn(req), "ip:192.0.2.8"; got != want {
			t.Errorf("keyFn() = %q, want %q", got, want)
		}
	})
}

func TestWSThrottleExhaustion(t *testing.T) {
	// Arrange
	store := newTestStore(t)
	issuer := testIssuer(t)
	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	var ran bool
	mw := RateLimit(store, LimitPolicy{
		Scope:  "ws_connect_test",
		Limit:  2,
		Window: time.Minute,
		KeyFn:  wsThrottleKey(issuer),
	})(probeHandler(&ran))

	req := func() *http.Request { return httptest.NewRequest("GET", "/api/v1/socket?token="+token, nil) }

	// Act: two requests under the limit
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req())
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req())

	// Assert
	if rec1.Code != 200 || rec2.Code != 200 {
		t.Fatalf("under-limit requests: got %d, %d, want 200, 200", rec1.Code, rec2.Code)
	}

	// Act: a third request over the limit
	ran = false
	rec3 := httptest.NewRecorder()
	mw.ServeHTTP(rec3, req())

	// Assert
	if rec3.Code != 429 {
		t.Fatalf("3rd: status = %d, want 429", rec3.Code)
	}
	if ran {
		t.Error("3rd: handler ran, want it not to")
	}
}

func TestWSRouteRateLimitHeaders(t *testing.T) {
	// Arrange
	deps := testDeps(t)
	mux := NewMux(deps)

	token, err := deps.Issuer.Issue(auth.Claims{UserID: "u2", DisplayName: "Grace", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	// Act: a plain (non-upgrade) request, so the throttle's headers are
	// visible on the recorder before Upgrade's hijack would otherwise
	// bypass the normal ResponseWriter path
	req := httptest.NewRequest("GET", "/api/v1/socket?token="+token, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Assert: the socket route is throttled, proven by the presence of
	// the shared rate limiter's response headers
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Fatal("no X-RateLimit-Limit header on the socket route — wsThrottle is not wired in")
	}
}
