package ws

import "time"

// DropCounter counts one event each call. Satisfied structurally by
// *metrics.Counter — internal/ws does not import internal/metrics.
type DropCounter interface {
	Inc()
}

// Room is a per-room owner goroutine. All state is owned by run() and
// mutated only through cmds — no mutex.
type Room struct {
	ID    string
	cmds  chan any
	done  chan struct{}
	sync  Recorder
	drops DropCounter
}

type joinCmd struct {
	c    *Client
	done chan struct{}
}

type leaveCmd struct {
	c *Client
}

type membersCmd struct {
	reply chan []Identity
}

type countCmd struct {
	reply chan int
}

type broadcastCmd struct {
	payload  []byte
	enqueued time.Time
}

type closeCmd struct {
	reply chan bool
}

type shutdownCmd struct {
	reply chan struct{}
}

// NewRoom starts the room's owner goroutine and returns immediately.
// sync is the recorder assigned to every joining client, for WritePump's
// enqueue-to-write latency observation; drops counts a payload dropped
// by a full send buffer. Either may be nil.
func NewRoom(id string, onEmpty func(roomID string), sync Recorder, drops DropCounter) *Room {
	r := &Room{ID: id, cmds: make(chan any), done: make(chan struct{}), sync: sync, drops: drops}
	go r.run(onEmpty)
	return r
}

// run owns the room's whole lifecycle. done closes when it returns —
// via the empty closeCmd branch below, the only exit — so a caller
// racing a send against reaping (Leave, Broadcast, Members, Count) can
// select on done instead of blocking on cmds forever: once run() has
// already decided to close, cmds has no reader left, and an unguarded
// send would hang. shutdownCmd's own exit is unreachable by that race
// (Hub.Shutdown is a one-time, process-ending call with no room-level
// caller left to race it), but closes done too since it is run()'s
// only other return.
func (r *Room) run(onEmpty func(roomID string)) {
	defer close(r.done)
	clients := make(map[*Client]struct{})

	for cmd := range r.cmds {
		switch c := cmd.(type) {
		case joinCmd:
			c.c.sync = r.sync
			clients[c.c] = struct{}{}
			close(c.done)
		case leaveCmd:
			removeAndNotify(clients, c.c, r.ID, onEmpty)
		case broadcastCmd:
			var evicted []*Client
			for client := range clients {
				select {
				case client.send <- outbound{payload: c.payload, enqueued: c.enqueued}:
				default:
					evicted = append(evicted, client)
					if r.drops != nil {
						r.drops.Inc()
					}
				}
			}
			for _, client := range evicted {
				removeAndNotify(clients, client, r.ID, onEmpty)
			}
		case membersCmd:
			c.reply <- membersOf(clients)
		case countCmd:
			c.reply <- len(clients)
		case closeCmd:
			if len(clients) == 0 {
				c.reply <- true
				return
			}
			c.reply <- false
		case shutdownCmd:
			for client := range clients {
				close(client.send)
			}
			c.reply <- struct{}{}
			return
		}
	}
}

// removeAndNotify deletes c from clients and closes its send channel,
// but only if c is actually present — a non-member removal is a no-op,
// which is what makes closing send here safe from a double-close
// panic. On a transition to zero members it calls onEmpty(roomID) in
// its own goroutine: a synchronous call would deadlock this room's own
// run() goroutine against the hub's reply command (Task 5).
func removeAndNotify(clients map[*Client]struct{}, c *Client, roomID string, onEmpty func(string)) {
	if _, ok := clients[c]; !ok {
		return
	}
	delete(clients, c)
	close(c.send)
	if len(clients) == 0 && onEmpty != nil {
		go onEmpty(roomID)
	}
}

func membersOf(clients map[*Client]struct{}) []Identity {
	out := make([]Identity, 0, len(clients))
	for c := range clients {
		out = append(out, c.Identity)
	}
	return out
}

// Join adds c to the room's membership and does not return until run()
// has finished processing the join — including assigning c's latency
// recorder — so a caller that immediately starts c's pumps can never
// race run()'s write to c's fields. Joining the same client pointer
// twice is a no-op.
func (r *Room) Join(c *Client) {
	done := make(chan struct{})
	r.cmds <- joinCmd{c: c, done: done}
	<-done
}

// Leave removes c from the room's membership and closes its send
// channel. A client that is not a member is a harmless no-op, and so is
// a call arriving after the room has already reaped itself (r.done):
// without that guard, this send would block forever on cmds once run()
// has no reader left — the exact hang a caller two steps behind a
// concurrent reap could otherwise hit.
func (r *Room) Leave(c *Client) {
	select {
	case r.cmds <- leaveCmd{c: c}:
	case <-r.done:
	}
}

// Broadcast delivers payload to every member's send channel,
// non-blocking — a member whose buffer is full does not stall this
// call, and is instead evicted and counted as a drop. The enqueue
// timestamp is stamped once, here, so the sync-latency metric includes
// time spent waiting on this room's own command channel, not only time
// in a client's send buffer. A room that has already reaped itself
// (r.done) makes this a silent no-op too, same as Leave — the room
// being gone is not this call's problem to report.
func (r *Room) Broadcast(payload []byte) {
	select {
	case r.cmds <- broadcastCmd{payload: payload, enqueued: time.Now()}:
	case <-r.done:
	}
}

// Members returns a snapshot of the room's current identities, in
// unspecified order — nil if the room has already reaped itself.
func (r *Room) Members() []Identity {
	reply := make(chan []Identity)
	select {
	case r.cmds <- membersCmd{reply: reply}:
	case <-r.done:
		return nil
	}
	return <-reply
}

// close asks the room to shut down its run() goroutine, but only if it
// is currently empty — it replies false and keeps running otherwise
// (a client may have rejoined between the empty notification and this
// call). Unexported: only the hub calls this, as part of reaping.
func (r *Room) close() bool {
	reply := make(chan bool)
	r.cmds <- closeCmd{reply: reply}
	return <-reply
}

// shutdown disconnects every member and unconditionally ends run(),
// regardless of membership. Unexported: only the hub calls this.
func (r *Room) shutdown() {
	reply := make(chan struct{})
	r.cmds <- shutdownCmd{reply: reply}
	<-reply
}

// Count returns the number of clients currently in the room.
func (r *Room) Count() int {
	reply := make(chan int)
	select {
	case r.cmds <- countCmd{reply: reply}:
	case <-r.done:
		return 0
	}
	return <-reply
}
