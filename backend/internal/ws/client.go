package ws

import "time"

// Conn is the subset of *websocket.Conn this package depends on, so
// pump tests can run against a stub without a real network connection.
type Conn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetReadLimit(limit int64)
	SetPongHandler(h func(appData string) error)
	Close() error
}

// Identity is who a connected client is, as carried by their token.
type Identity struct {
	UserID      string
	DisplayName string
	Guest       bool
}

// Client is one connected socket: its identity, its underlying
// connection, and the buffered channel its write pump drains.
type Client struct {
	Identity
	conn Conn
	send chan []byte
}

// newClient constructs a Client with a send buffer of the given size.
// conn may be nil in tests that never exercise the pumps.
func newClient(conn Conn, ident Identity, sendBuffer int) *Client {
	return &Client{
		Identity: ident,
		conn:     conn,
		send:     make(chan []byte, sendBuffer),
	}
}
