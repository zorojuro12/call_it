package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

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
	cfg  ClientConfig
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

// ClientConfig tunes a client's pumps: buffering, heartbeat cadence,
// and deadlines.
type ClientConfig struct {
	SendBuffer   int
	PingInterval time.Duration
	PongWait     time.Duration
	WriteWait    time.Duration
	MaxMessage   int64
}

// DefaultClientConfig returns production-sane pump tuning.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		SendBuffer:   64,
		PingInterval: 30 * time.Second,
		PongWait:     60 * time.Second,
		WriteWait:    10 * time.Second,
		MaxMessage:   4096,
	}
}

// NewClient constructs a Client ready for its pumps to be started.
func NewClient(conn Conn, ident Identity, cfg ClientConfig) *Client {
	return &Client{
		Identity: ident,
		conn:     conn,
		cfg:      cfg,
		send:     make(chan []byte, cfg.SendBuffer),
	}
}

// WritePump drains c.send, writing each payload as a text message, and
// sends a heartbeat ping on cfg.PingInterval. It returns — closing the
// connection — when send is closed or a write fails.
func (c *Client) WritePump() {
	defer c.conn.Close()

	ticker := time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(c.cfg.WriteWait))
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.cfg.WriteWait)); err != nil {
				return
			}
		}
	}
}
