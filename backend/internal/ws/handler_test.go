package ws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
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
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
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
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
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

func TestPresenceJoin(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
	defer server.Close()

	tokenA, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	tokenB, err := issuer.Issue(auth.Claims{UserID: "u2", DisplayName: "Grace", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	// Act: A connects and drains its connected event
	connA, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenA, nil)
	if err != nil {
		t.Fatalf("Dial A error: %v", err)
	}
	defer connA.Close()
	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, connA, TypeConnected)

	// A also receives a player_joined about its own join — the design
	// deliberately broadcasts to the whole room including the newcomer
	// (Task 7 CP1 contract), rather than special-casing the sender.
	selfEnv := mustReadEnvelope(t, connA, TypePlayerJoined)
	var selfPresence PresenceEvent
	if err := unmarshalData(selfEnv, &selfPresence); err != nil {
		t.Fatalf("unmarshal self PresenceEvent: %v", err)
	}
	wantSelf := PresenceEvent{UserID: "u1", DisplayName: "Ada", PlayerCount: 1}
	if selfPresence != wantSelf {
		t.Fatalf("A's own player_joined = %+v, want %+v", selfPresence, wantSelf)
	}

	// Act: B connects
	connB, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenB, nil)
	if err != nil {
		t.Fatalf("Dial B error: %v", err)
	}
	defer connB.Close()
	connB.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Assert: B receives its own connected event first
	mustReadEnvelope(t, connB, TypeConnected)

	// Assert: B's next message is the roster snapshot telling it about A,
	// who was already in the room (TestPresenceJoinTellsNewcomerAboutExistingMembers
	// covers this in isolation; this just consumes it in sequence here).
	mustReadEnvelope(t, connB, TypePlayerJoined)

	// Assert: A's next message is player_joined for B
	env := mustReadEnvelope(t, connA, TypePlayerJoined)
	var presence PresenceEvent
	if err := unmarshalData(env, &presence); err != nil {
		t.Fatalf("unmarshal PresenceEvent: %v", err)
	}
	want := PresenceEvent{UserID: "u2", DisplayName: "Grace", PlayerCount: 2}
	if presence != want {
		t.Fatalf("A's player_joined = %+v, want %+v", presence, want)
	}

	// Assert: B also receives the same player_joined broadcast, about itself
	env = mustReadEnvelope(t, connB, TypePlayerJoined)
	if err := unmarshalData(env, &presence); err != nil {
		t.Fatalf("unmarshal PresenceEvent: %v", err)
	}
	if presence != want {
		t.Fatalf("B's player_joined = %+v, want %+v", presence, want)
	}
}

func TestPresenceJoinTellsNewcomerAboutExistingMembers(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
	defer server.Close()

	tokenA, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	tokenB, err := issuer.Issue(auth.Claims{UserID: "u2", DisplayName: "Grace", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	// Act: A connects and drains its connected + self player_joined
	connA, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenA, nil)
	if err != nil {
		t.Fatalf("Dial A error: %v", err)
	}
	defer connA.Close()
	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, connA, TypeConnected)
	mustReadEnvelope(t, connA, TypePlayerJoined)

	// Act: B connects into a room that already has A in it
	connB, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenB, nil)
	if err != nil {
		t.Fatalf("Dial B error: %v", err)
	}
	defer connB.Close()
	connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, connB, TypeConnected)

	// Assert: B's very next message tells it about A, the pre-existing
	// member — not just future joins/leaves. Without this, a client that
	// joins second never learns who is already in the room.
	rosterEnv := mustReadEnvelope(t, connB, TypePlayerJoined)
	var rosterPresence PresenceEvent
	if err := unmarshalData(rosterEnv, &rosterPresence); err != nil {
		t.Fatalf("unmarshal roster PresenceEvent: %v", err)
	}
	wantRoster := PresenceEvent{UserID: "u1", DisplayName: "Ada", PlayerCount: 2}
	if rosterPresence != wantRoster {
		t.Fatalf("B's roster player_joined = %+v, want %+v", rosterPresence, wantRoster)
	}

	// Assert: B's message after that is its own self-broadcast, unchanged.
	selfEnv := mustReadEnvelope(t, connB, TypePlayerJoined)
	var selfPresence PresenceEvent
	if err := unmarshalData(selfEnv, &selfPresence); err != nil {
		t.Fatalf("unmarshal self PresenceEvent: %v", err)
	}
	wantSelf := PresenceEvent{UserID: "u2", DisplayName: "Grace", PlayerCount: 2}
	if selfPresence != wantSelf {
		t.Fatalf("B's self player_joined = %+v, want %+v", selfPresence, wantSelf)
	}
}

func TestPresenceLeave(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
	defer server.Close()

	tokenA, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	tokenB, err := issuer.Issue(auth.Claims{UserID: "u2", DisplayName: "Grace", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	connA, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenA, nil)
	if err != nil {
		t.Fatalf("Dial A error: %v", err)
	}
	defer connA.Close()
	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, connA, TypeConnected)
	mustReadEnvelope(t, connA, TypePlayerJoined) // A's own self-broadcast

	connB, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+tokenB, nil)
	if err != nil {
		t.Fatalf("Dial B error: %v", err)
	}
	connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, connB, TypeConnected)
	mustReadEnvelope(t, connB, TypePlayerJoined) // B's roster snapshot, telling it about A
	mustReadEnvelope(t, connB, TypePlayerJoined) // B's own broadcast, reaching B

	mustReadEnvelope(t, connA, TypePlayerJoined) // A also hears B's join

	// Act: close B's connection
	connB.Close()

	// Assert: A's next message is player_left for B, count taken after removal
	env := mustReadEnvelope(t, connA, TypePlayerLeft)
	var presence PresenceEvent
	if err := unmarshalData(env, &presence); err != nil {
		t.Fatalf("unmarshal PresenceEvent: %v", err)
	}
	want := PresenceEvent{UserID: "u2", DisplayName: "Grace", PlayerCount: 1}
	if presence != want {
		t.Fatalf("A's player_left = %+v, want %+v", presence, want)
	}

	// Act: close A's connection too
	connA.Close()

	// Assert: the room is reaped
	waitFor(t, func() bool { return hub.RoomCount() == 0 })
}

func TestHandlerHostClaim(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
	defer server.Close()

	hostToken, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false, Host: true})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	playerToken, err := issuer.Issue(auth.Claims{UserID: "u2", DisplayName: "Bob", RoomID: "r1", Guest: false, Host: false})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	t.Run("host connects with host true", func(t *testing.T) {
		// Act
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+hostToken, nil)
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		env := mustReadEnvelope(t, conn, TypeConnected)
		var got ConnectedEvent
		if err := unmarshalData(env, &got); err != nil {
			t.Fatalf("unmarshal ConnectedEvent: %v", err)
		}

		// Assert
		if !got.Host {
			t.Fatalf("Host = %v, want true", got.Host)
		}
	})

	t.Run("player connects with host false", func(t *testing.T) {
		// Act
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+playerToken, nil)
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		env := mustReadEnvelope(t, conn, TypeConnected)
		var got ConnectedEvent
		if err := unmarshalData(env, &got); err != nil {
			t.Fatalf("unmarshal ConnectedEvent: %v", err)
		}

		// Assert
		if got.Host {
			t.Fatalf("Host = %v, want false", got.Host)
		}
	})
}

func TestHandlerAllowedOrigins(t *testing.T) {
	// Arrange
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil, WithAllowedOrigins([]string{"http://localhost:3000"})))
	defer server.Close()

	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	t.Run("an allowed origin succeeds", func(t *testing.T) {
		// Act
		header := http.Header{"Origin": []string{"http://localhost:3000"}}
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, header)

		// Assert
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
		}
		env := mustReadEnvelope(t, conn, TypeConnected)
		var got ConnectedEvent
		if err := unmarshalData(env, &got); err != nil {
			t.Fatalf("unmarshal ConnectedEvent: %v", err)
		}
	})

	t.Run("a disallowed origin is rejected with 403", func(t *testing.T) {
		// Act
		header := http.Header{"Origin": []string{"http://evil.test"}}
		_, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, header)

		// Assert
		if err == nil {
			t.Fatal("Dial error = nil, want a rejected upgrade")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			status := -1
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
		}
	})

	t.Run("no Origin header succeeds, for non-browser clients", func(t *testing.T) {
		// Act
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, nil)

		// Assert
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
		}
	})

	t.Run("with no WithAllowedOrigins option, the pre-existing same-origin default is preserved", func(t *testing.T) {
		// Arrange
		defaultServer := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, nil))
		defer defaultServer.Close()

		// Act
		header := http.Header{"Origin": []string{"http://evil.test"}}
		_, resp, err := websocket.DefaultDialer.Dial(wsURL(defaultServer.URL)+"?token="+token, header)

		// Assert
		if err == nil {
			t.Fatal("Dial error = nil, want a rejected upgrade")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			status := -1
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
		}
	})
}

func mustReadEnvelope(t *testing.T, conn *websocket.Conn, wantType string) Envelope {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage error: %v", err)
	}
	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if env.Type != wantType {
		t.Fatalf("Type = %q, want %q", env.Type, wantType)
	}
	return env
}

func unmarshalData(env Envelope, v any) error {
	return json.Unmarshal(env.Data, v)
}

// fakeSessions records ResumeSession and ScheduleEndSession calls
// instead of touching any real store — the handler-level tests care
// only about which calls the connect/disconnect paths make and with
// what arguments, not about round.Service's actual fold behavior
// (covered by internal/round's own tests).
type fakeSessions struct {
	mu        sync.Mutex
	resumed   []string
	scheduled []string
}

func (f *fakeSessions) ResumeSession(roomID, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, roomID+"/"+userID)
}

func (f *fakeSessions) ScheduleEndSession(roomID, userID string, guest bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduled = append(f.scheduled, fmt.Sprintf("%s/%s/%v", roomID, userID, guest))
}

func TestHandlerSchedulesSessionEndOnDisconnect(t *testing.T) {
	issuer := newTestIssuer(t, time.Hour)
	hub := NewHub(nil, nil)
	fake := &fakeSessions{}
	server := httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, fake))
	defer server.Close()

	token, err := issuer.Issue(auth.Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1"})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token="+token, nil)
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mustReadEnvelope(t, conn, TypeConnected)
	mustReadEnvelope(t, conn, TypePlayerJoined)

	conn.Close()

	waitFor(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.scheduled) > 0
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.scheduled) != 1 {
		t.Fatalf("scheduled = %v, want exactly one entry", fake.scheduled)
	}
	want := "r1/u1/false"
	if fake.scheduled[0] != want {
		t.Errorf("scheduled[0] = %q, want %q", fake.scheduled[0], want)
	}
}
