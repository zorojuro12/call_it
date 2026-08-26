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
