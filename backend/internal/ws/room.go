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

type membersCmd struct {
	reply chan []Identity
}

type countCmd struct {
	reply chan int
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
		case membersCmd:
			c.reply <- membersOf(clients)
		case countCmd:
			c.reply <- len(clients)
		}
	}
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
