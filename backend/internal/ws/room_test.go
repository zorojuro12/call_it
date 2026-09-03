package ws

import (
	"testing"
	"time"
)

func TestRoomJoin(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil, nil, nil)
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
	room := NewRoom("r1", nil, nil, nil)
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
	room := NewRoom("r1", nil, nil, nil)
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
		if string(first.payload) != "hello" || string(second.payload) != "world" {
			t.Fatalf("client %s got [%q, %q], want [\"hello\", \"world\"]", c.UserID, first.payload, second.payload)
		}
		select {
		case extra := <-c.send:
			t.Fatalf("client %s got unexpected extra message %q", c.UserID, extra.payload)
		default:
		}
	}
}

func TestRoomEvicts(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil, nil, nil)
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
	if string(buffered.payload) != "one" {
		t.Fatalf("slow buffered payload = %q, want \"one\"", buffered.payload)
	}
	if _, ok := <-slow.send; ok {
		t.Fatalf("slow.send should be closed after eviction")
	}

	// Assert: fast received both payloads — the broadcast was never blocked by slow
	first := <-fast.send
	second := <-fast.send
	if string(first.payload) != "one" || string(second.payload) != "two" {
		t.Fatalf("fast got [%q, %q], want [\"one\", \"two\"]", first.payload, second.payload)
	}
}

func TestRoomOnEmpty(t *testing.T) {
	// Arrange
	notified := make(chan string, 4)
	room := NewRoom("r1", func(roomID string) { notified <- roomID }, nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	c2 := newClient(nil, Identity{UserID: "u2"}, 4)
	room.Join(c1)
	room.Join(c2)

	// Act: leaving the first of two members
	room.Leave(c1)

	// Assert: no notification yet
	select {
	case roomID := <-notified:
		t.Fatalf("unexpected notification %q after first leave", roomID)
	case <-time.After(100 * time.Millisecond):
	}

	// Act: leaving the second member empties the room
	room.Leave(c2)

	// Assert: notification arrives
	select {
	case roomID := <-notified:
		if roomID != "r1" {
			t.Fatalf("notified roomID = %q, want \"r1\"", roomID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification after emptying room, got none")
	}

	// Act: leaving the already-departed second member again
	room.Leave(c2)

	// Assert: no further notification
	select {
	case roomID := <-notified:
		t.Fatalf("unexpected second notification %q", roomID)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRoomOnEmptyNilIsLegal(t *testing.T) {
	// Arrange
	room := NewRoom("r1", nil, nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	room.Join(c1)

	// Act & Assert: must not panic
	room.Leave(c1)
	if got := room.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
}

// TestBroadcastAfterReapDoesNotHang is a regression test for a race
// found while writing Phase 7c's disconnect-grace-window test: when the
// last member leaves, removeAndNotify's onEmpty runs in its own
// goroutine, concurrently with whatever the caller of Leave does next.
// If that next call is Broadcast (as the WS handler's disconnect path
// always does, to announce player_left) and onEmpty's round trip
// reaches this room's own close() and returns before Broadcast's send
// arrives, run() has already exited and nobody is left reading cmds —
// an unguarded send would block forever. onEmpty here calls room.close()
// directly, standing in for the hub's real reap step, to force that
// ordering deterministically instead of hoping a real race reproduces.
func TestBroadcastAfterReapDoesNotHang(t *testing.T) {
	var room *Room
	room = NewRoom("r1", func(roomID string) { room.close() }, nil, nil)
	c1 := newClient(nil, Identity{UserID: "u1"}, 4)
	room.Join(c1)

	room.Leave(c1)
	waitFor(t, func() bool {
		select {
		case <-room.done:
			return true
		default:
			return false
		}
	})

	done := make(chan struct{})
	go func() {
		room.Broadcast([]byte("late broadcast"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast after the room reaped itself did not return — regression of the room.done guard")
	}
}

// stubDropCounter counts Inc() calls.
type stubDropCounter struct {
	calls int
}

func (d *stubDropCounter) Inc() {
	d.calls++
}

// TestJoinSyncsClientBeforeReturning is a security-review regression
// test (found while closing Phase 7a): Join must not return until
// run() has finished writing c.sync, or a caller that immediately
// starts WritePump — as Handler always does — races run()'s write
// against WritePump's read of the same field, with no happens-before
// edge between them. A bare channel send/receive rendezvous only
// orders the value transfer, not run()'s subsequent statements, so this
// needs an explicit acknowledgement.
func TestJoinSyncsClientBeforeReturning(t *testing.T) {
	for i := 0; i < 200; i++ {
		recorder := &stubSyncRecorder{}
		room := NewRoom("r1", nil, recorder, nil)
		c := newClient(nil, Identity{UserID: "u1"}, 4)

		room.Join(c)

		if c.sync != recorder {
			t.Fatalf("iteration %d: c.sync = %v immediately after Join, want the room's recorder already set", i, c.sync)
		}
		go func() { _ = c.sync }()
	}
}

func TestBroadcastCountsDroppedClient(t *testing.T) {
	// Arrange: a full send buffer and no WritePump draining it, so the
	// non-blocking send in Room.run's broadcastCmd case must hit its
	// default branch.
	recorder := &stubSyncRecorder{}
	drops := &stubDropCounter{}
	room := NewRoom("r1", nil, recorder, drops)
	slow := newClient(nil, Identity{UserID: "slow"}, 1)
	room.Join(slow)
	slow.send <- outbound{payload: []byte("fills the one slot")}

	// Act
	room.Broadcast([]byte("evicts slow"))

	// Assert
	waitFor(t, func() bool { return room.Count() == 0 })
	if got := room.Count(); got != 0 {
		t.Fatalf("Count() after eviction = %d, want 0", got)
	}
	if drops.calls != 1 {
		t.Fatalf("drop counter calls = %d, want 1", drops.calls)
	}
	if len(recorder.calls()) != 0 {
		t.Fatalf("latency recorder observed %d calls, want 0", len(recorder.calls()))
	}
}
