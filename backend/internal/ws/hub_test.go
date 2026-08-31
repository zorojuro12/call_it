package ws

import (
	"testing"
	"time"
)

func TestHubBroadcastByID(t *testing.T) {
	h := NewHub(nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1", DisplayName: "Ada"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2", DisplayName: "Grace"}, 4)
	c3 := newClient(nil, Identity{UserID: "u3", DisplayName: "Margaret"}, 4)
	h.Join("r1", c1)
	h.Join("r1", c2)
	h.Join("r2", c3)

	payload := []byte(`{"type":"test"}`)
	h.Broadcast("r1", payload)

	for _, c := range []*Client{c1, c2} {
		select {
		case got := <-c.send:
			if string(got.payload) != string(payload) {
				t.Errorf("client %s got %s, want %s", c.UserID, got.payload, payload)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("client %s received nothing, want the broadcast payload", c.UserID)
		}
	}
	select {
	case got := <-c3.send:
		t.Errorf("client %s (in a different room) received %s, want nothing", c3.UserID, got.payload)
	case <-time.After(20 * time.Millisecond):
	}

	// Must not panic.
	h.Broadcast("nonexistent", payload)

	names := h.Names("r1")
	want := map[string]string{"u1": "Ada", "u2": "Grace"}
	if len(names) != len(want) || names["u1"] != want["u1"] || names["u2"] != want["u2"] {
		t.Errorf("Names(r1) = %v, want %v", names, want)
	}

	empty := h.Names("nonexistent")
	if empty == nil || len(empty) != 0 {
		t.Errorf("Names(nonexistent) = %v, want an empty non-nil map", empty)
	}
}

func TestHubJoin(t *testing.T) {
	// Arrange
	h := NewHub(nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2"}, 4)
	c3 := newClient(nil, Identity{UserID: "u3"}, 4)

	// Act
	roomA := h.Join("r1", c1)

	// Assert
	if roomA.ID != "r1" {
		t.Fatalf("roomA.ID = %q, want %q", roomA.ID, "r1")
	}
	if got := h.RoomCount(); got != 1 {
		t.Fatalf("RoomCount() = %d, want 1", got)
	}
	if got := roomA.Count(); got != 1 {
		t.Fatalf("roomA.Count() = %d, want 1", got)
	}

	// Act
	roomB := h.Join("r1", c2)

	// Assert
	if roomB != roomA {
		t.Fatalf("roomB != roomA, want the same room reused")
	}
	if got := h.RoomCount(); got != 1 {
		t.Fatalf("RoomCount() after rejoin = %d, want 1", got)
	}
	if got := roomA.Count(); got != 2 {
		t.Fatalf("roomA.Count() after second join = %d, want 2", got)
	}

	// Act
	roomC := h.Join("r2", c3)

	// Assert
	if roomC == roomA {
		t.Fatalf("roomC == roomA, want a distinct room for a different ID")
	}
	if got := h.RoomCount(); got != 2 {
		t.Fatalf("RoomCount() after new room = %d, want 2", got)
	}
}

func TestHubReaps(t *testing.T) {
	// Arrange
	h := NewHub(nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2"}, 4)
	firstRoom := h.Join("r1", c1)
	h.Join("r1", c2)
	if got := h.RoomCount(); got != 1 {
		t.Fatalf("RoomCount() = %d, want 1", got)
	}

	// Act
	firstRoom.Leave(c1)

	// Assert: still one member, room not reaped
	waitForCount(t, h, 1, 200*time.Millisecond)

	// Act
	firstRoom.Leave(c2)

	// Assert: reaped — poll, since the notification is asynchronous by design
	waitForCount(t, h, 0, 500*time.Millisecond)

	// Act
	newRoom := h.Join("r1", newClient(nil, Identity{UserID: "u3"}, 4))

	// Assert: a fresh room, not the reaped one
	if got := h.RoomCount(); got != 1 {
		t.Fatalf("RoomCount() after rejoin = %d, want 1", got)
	}
	if newRoom == firstRoom {
		t.Fatal("rejoin returned the reaped room, want a different pointer")
	}
}

func TestHubShutdown(t *testing.T) {
	// Arrange
	h := NewHub(nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2"}, 4)
	h.Join("r1", c1)
	h.Join("r2", c2)

	// Act
	h.Shutdown()

	// Assert: immediately after Shutdown returns, no sleep
	if got := h.RoomCount(); got != 0 {
		t.Fatalf("RoomCount() after Shutdown = %d, want 0", got)
	}
	if _, ok := <-c1.send; ok {
		t.Error("c1.send should be closed after Shutdown")
	}
	if _, ok := <-c2.send; ok {
		t.Error("c2.send should be closed after Shutdown")
	}
}

// waitForCount polls h.RoomCount() until it equals want or timeout
// elapses, failing the test on timeout.
func waitForCount(t *testing.T, h *Hub, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.RoomCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := h.RoomCount(); got != want {
		t.Fatalf("RoomCount() = %d, want %d", got, want)
	}
}
