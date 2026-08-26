package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

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
