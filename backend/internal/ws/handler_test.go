package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/auth"
)

const testSecret = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"  // 32 bytes
const otherSecret = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" // 32 bytes, different from testSecret

func newTestIssuer(t *testing.T, ttl time.Duration) *auth.Issuer {
	t.Helper()
	issuer, err := auth.NewIssuer([]byte(testSecret), ttl)
	if err != nil {
		t.Fatalf("auth.NewIssuer error: %v", err)
	}
	return issuer
}

func newOtherSecretIssuer(t *testing.T, ttl time.Duration) *auth.Issuer {
	t.Helper()
	issuer, err := auth.NewIssuer([]byte(otherSecret), ttl)
	if err != nil {
		t.Fatalf("auth.NewIssuer error: %v", err)
	}
	return issuer
}

func wsURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}

func TestHandlerUpgrade(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub()
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil))
	defer server.Close()

	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	t.Run("query parameter", func(t *testing.T) {
		// Act
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, nil)
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()

		// Assert
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage error: %v", err)
		}
		env, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode error: %v", err)
		}
		if env.Type != TypeConnected {
			t.Fatalf("Type = %q, want %q", env.Type, TypeConnected)
		}
		var got ConnectedEvent
		if err := unmarshalData(env, &got); err != nil {
			t.Fatalf("unmarshal ConnectedEvent: %v", err)
		}
		want := ConnectedEvent{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		if hub.RoomCount() != 1 {
			t.Fatalf("RoomCount() = %d, want 1", hub.RoomCount())
		}
	})

	t.Run("authorization header", func(t *testing.T) {
		// Act
		header := http.Header{"Authorization": []string{"Bearer " + token}}
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL), header)
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()

		// Assert
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage error: %v", err)
		}
		env, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode error: %v", err)
		}
		if env.Type != TypeConnected {
			t.Fatalf("Type = %q, want %q", env.Type, TypeConnected)
		}
		var got ConnectedEvent
		if err := unmarshalData(env, &got); err != nil {
			t.Fatalf("unmarshal ConnectedEvent: %v", err)
		}
		want := ConnectedEvent{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
}

func TestHandlerRejects(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	otherIssuer := newOtherSecretIssuer(t, time.Hour)
	expiredIssuer := newTestIssuer(t, time.Millisecond)
	hub := NewHub()
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil))
	defer server.Close()

	wrongSecretToken, err := otherIssuer.Issue(auth.Claims{UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	expiredToken, err := expiredIssuer.Issue(auth.Claims{UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	tests := []struct {
		name  string
		query string
	}{
		{"absent", ""},
		{"garbage", "?token=not-a-jwt"},
		{"wrong secret", "?token=" + wrongSecretToken},
		{"expired", "?token=" + expiredToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+tt.query, nil)

			// Assert
			if err == nil {
				t.Fatal("Dial succeeded, want failure")
			}
			if resp == nil || resp.StatusCode != http.StatusUnauthorized {
				status := -1
				if resp != nil {
					status = resp.StatusCode
				}
				t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
			}
			if hub.RoomCount() != 0 {
				t.Fatalf("RoomCount() = %d, want 0", hub.RoomCount())
			}
		})
	}
}

func TestHandlerRequiresRoom(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub()
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil))
	defer server.Close()

	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: ""})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	// Act
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, nil)

	// Assert
	if err == nil {
		t.Fatal("Dial succeeded, want failure")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
	if hub.RoomCount() != 0 {
		t.Fatalf("RoomCount() = %d, want 0", hub.RoomCount())
	}
}

func unmarshalData(env Envelope, v any) error {
	return json.Unmarshal(env.Data, v)
}
