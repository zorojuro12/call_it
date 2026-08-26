package ws

// Room is a per-room owner goroutine. All state is owned by run() and
// mutated only through cmds — no mutex.
type Room struct {
	ID   string
	cmds chan any
}

type joinCmd struct {
	c *Client
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
	payload []byte
}

// NewRoom starts the room's owner goroutine and returns immediately.
func NewRoom(id string, onEmpty func(roomID string)) *Room {
	r := &Room{ID: id, cmds: make(chan any)}
	go r.run(onEmpty)
	return r
}

func (r *Room) run(onEmpty func(roomID string)) {
	clients := make(map[*Client]struct{})

	for cmd := range r.cmds {
		switch c := cmd.(type) {
		case joinCmd:
			clients[c.c] = struct{}{}
		case leaveCmd:
			remove(clients, c.c)
		case broadcastCmd:
			var evicted []*Client
			for client := range clients {
				select {
				case client.send <- c.payload:
				default:
					evicted = append(evicted, client)
				}
			}
			for _, client := range evicted {
				remove(clients, client)
			}
		case membersCmd:
			c.reply <- membersOf(clients)
		case countCmd:
			c.reply <- len(clients)
		}
	}
}

// remove deletes c from clients and closes its send channel, but only
// if c is actually present — a non-member removal is a no-op, which is
// what makes closing send here safe from a double-close panic.
func remove(clients map[*Client]struct{}, c *Client) {
	if _, ok := clients[c]; !ok {
		return
	}
	delete(clients, c)
	close(c.send)
}

func membersOf(clients map[*Client]struct{}) []Identity {
	out := make([]Identity, 0, len(clients))
	for c := range clients {
		out = append(out, c.Identity)
	}
	return out
}

// Join adds c to the room's membership. Joining the same client
// pointer twice is a no-op.
func (r *Room) Join(c *Client) {
	r.cmds <- joinCmd{c: c}
}

// Leave removes c from the room's membership and closes its send
// channel. A client that is not a member is a harmless no-op.
func (r *Room) Leave(c *Client) {
	r.cmds <- leaveCmd{c: c}
}

// Broadcast delivers payload to every member's send channel,
// non-blocking — a member whose buffer is full does not stall this
// call (eviction of that member is pinned by a later checkpoint).
func (r *Room) Broadcast(payload []byte) {
	r.cmds <- broadcastCmd{payload: payload}
}

// Members returns a snapshot of the room's current identities, in
// unspecified order.
func (r *Room) Members() []Identity {
	reply := make(chan []Identity)
	r.cmds <- membersCmd{reply: reply}
	return <-reply
}

// Count returns the number of clients currently in the room.
func (r *Room) Count() int {
	reply := make(chan int)
	r.cmds <- countCmd{reply: reply}
	return <-reply
}
