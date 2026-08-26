package ws

import "testing"

func TestHubJoin(t *testing.T) {
	// Arrange
	h := NewHub()
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
