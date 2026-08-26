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

func TestRoomBroadcast(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2"}, 4)
	c3 := newClient(nil, Identity{UserID: "u3"}, 4)
	room.Join(c1)
	room.Join(c2)
	room.Join(c3)

	// Act
	room.Broadcast([]byte("hello"))
	room.Broadcast([]byte("world"))

	// Assert
	for _, c := range []*Client{c1, c2, c3} {
		first := <-c.send
		second := <-c.send
		if string(first) != "hello" || string(second) != "world" {
			t.Fatalf("client %s got [%q, %q], want [\"hello\", \"world\"]", c.UserID, first, second)
		}
		select {
		case extra := <-c.send:
			t.Fatalf("client %s got unexpected extra message %q", c.UserID, extra)
		default:
		}
	}
}

func TestRoomEvicts(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil)
	slow := newClient(nil, Identity{UserID: "slow"}, 1)
	fast := newClient(nil, Identity{UserID: "fast"}, 8)
	room.Join(slow)
	room.Join(fast)

	// Act: first broadcast fills slow's single slot
	room.Broadcast([]byte("one"))
	// Act: second broadcast finds slow full and evicts it
	room.Broadcast([]byte("two"))

	// Assert: slow was evicted
	if got := room.Count(); got != 1 {
		t.Fatalf("Count() after eviction = %d, want 1", got)
	}
	members := room.Members()
	if len(members) != 1 || members[0] != fast.Identity {
		t.Fatalf("Members() after eviction = %+v, want only %+v", members, fast.Identity)
	}

	// Assert: slow's send channel is closed, after draining its one buffered payload
	buffered := <-slow.send
	if string(buffered) != "one" {
		t.Fatalf("slow buffered payload = %q, want \"one\"", buffered)
	}
	if _, ok := <-slow.send; ok {
		t.Fatalf("slow.send should be closed after eviction")
	}

	// Assert: fast received both payloads — the broadcast was never blocked by slow
	first := <-fast.send
	second := <-fast.send
	if string(first) != "one" || string(second) != "two" {
		t.Fatalf("fast got [%q, %q], want [\"one\", \"two\"]", first, second)
	}
}
