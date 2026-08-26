package ws

// Hub is the room registry goroutine: get-or-create + join, empty-room
// reaping, and shutdown. No mutex — state is owned by run().
type Hub struct {
	cmds  chan any
	empty chan string
}

type hubJoinCmd struct {
	roomID string
	c      *Client
	reply  chan *Room
}

type hubRoomCountCmd struct {
	reply chan int
}

type hubShutdownCmd struct {
	reply chan struct{}
}

// NewHub starts the hub's owner goroutine and returns immediately.
func NewHub() *Hub {
	h := &Hub{
		cmds:  make(chan any),
		empty: make(chan string),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	rooms := make(map[string]*Room)

	for {
		select {
		case cmd := <-h.cmds:
			switch c := cmd.(type) {
			case hubJoinCmd:
				room, ok := rooms[c.roomID]
				if !ok {
					room = NewRoom(c.roomID, h.notifyEmpty)
					rooms[c.roomID] = room
				}
				room.Join(c.c)
				c.reply <- room
			case hubRoomCountCmd:
				c.reply <- len(rooms)
			case hubShutdownCmd:
				for _, room := range rooms {
					room.shutdown()
				}
				rooms = make(map[string]*Room)
				c.reply <- struct{}{}
			}
		case roomID := <-h.empty:
			if room, ok := rooms[roomID]; ok && room.close() {
				delete(rooms, roomID)
			}
		}
	}
}

// notifyEmpty is passed to each room as its onEmpty callback.
func (h *Hub) notifyEmpty(roomID string) {
	h.empty <- roomID
}

// Join gets or creates the room for roomID and joins c to it, as one
// atomic hub command — closing the window where the room could be
// reaped between a separate get and join.
func (h *Hub) Join(roomID string, c *Client) *Room {
	reply := make(chan *Room)
	h.cmds <- hubJoinCmd{roomID: roomID, c: c, reply: reply}
	return <-reply
}

// Shutdown disconnects every client in every room and returns only
// once that is complete — a synchronous guarantee for the process's
// graceful shutdown, not a hope. A Join after Shutdown is out of
// scope: the process is exiting.
func (h *Hub) Shutdown() {
	reply := make(chan struct{})
	h.cmds <- hubShutdownCmd{reply: reply}
	<-reply
}

// RoomCount returns the number of rooms currently registered.
func (h *Hub) RoomCount() int {
	reply := make(chan int)
	h.cmds <- hubRoomCountCmd{reply: reply}
	return <-reply
}
