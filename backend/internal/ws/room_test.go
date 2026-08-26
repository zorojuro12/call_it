package ws

import (
	"testing"
)

func TestRoomJoin(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil)
	c1 := newClient(nil, Identity{UserID: "u1", DisplayName: "Ada"}, 4)

	// Act
	room.Join(c1)

	// Assert
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	members := room.Members()
	want := []Identity{{UserID: "u1", DisplayName: "Ada"}}
	if len(members) != len(want) || members[0] != want[0] {
		t.Fatalf("Members() = %+v, want %+v", members, want)
	}

	// Act: joining the same client pointer twice
	room.Join(c1)

	// Assert: count unchanged
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() after duplicate join = %d, want 1", got)
	}
}

func TestRoomLeave(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil)
	c1 := newClient(nil, Identity{UserID: "u1", DisplayName: "Ada"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2", DisplayName: "Grace"}, 4)
	room.Join(c1)
	room.Join(c2)

	// Act
	room.Leave(c1)

	// Assert
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() after leave = %d, want 1", got)
	}
	members := room.Members()
	if len(members) != 1 || members[0] != c2.Identity {
		t.Fatalf("Members() after leave = %+v, want only %+v", members, c2.Identity)
	}

	// Act: leaving the same client again must not panic
	room.Leave(c1)

	// Assert
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() after double leave = %d, want 1", got)
	}

	// Act: leaving a client that never joined must not panic
	c3 := newClient(nil, Identity{UserID: "u3", DisplayName: "Lin"}, 4)
	room.Leave(c3)

	// Assert
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() after leaving non-member = %d, want 1", got)
	}
}
