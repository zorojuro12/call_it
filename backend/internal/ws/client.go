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

// Recorder observes one latency sample. Satisfied structurally by
// *metrics.Histogram — internal/ws does not import internal/metrics.
type Recorder interface {
	Observe(d time.Duration)
}

// outbound is one payload queued for delivery, carrying the time it was
// enqueued so WritePump can measure the queue-wait latency that
// contributes to spec §7's WebSocket sync target.
type outbound struct {
	payload  []byte
	enqueued time.Time
}

// Client is one connected socket: its identity, its underlying
// connection, and the buffered channel its write pump drains. RoomID is
// the room its token is scoped to — set by Handler from the verified
// claims, so a message router can trust it over anything a message
// payload claims.
type Client struct {
	Identity
	RoomID string
	conn   Conn
	cfg    ClientConfig
	send   chan outbound
	sync   Recorder
}

// newClient constructs a Client with a send buffer of the given size.
// conn may be nil in tests that never exercise the pumps.
func newClient(conn Conn, ident Identity, sendBuffer int) *Client {
	return &Client{
		Identity: ident,
		conn:     conn,
		send:     make(chan outbound, sendBuffer),
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
		send:     make(chan outbound, cfg.SendBuffer),
	}
}

// MessageHandler is the seam Phase 4b fills. A nil handler is legal
// and makes every inbound message an unknown-type error reply.
type MessageHandler func(c *Client, e Envelope)

// ReadPump loops on c.conn.ReadMessage, decoding and dispatching each
// message to handle. It returns on any read error.
func (c *Client) ReadPump(handle MessageHandler, onClose func()) {
	defer func() {
		c.conn.Close()
		if onClose != nil {
			onClose()
		}
	}()

	c.conn.SetReadLimit(c.cfg.MaxMessage)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		env, err := Decode(raw)
		if err != nil {
			c.Send(mustEncode(TypeError, ErrorEvent{Code: "malformed", Message: err.Error()}))
			continue
		}
		if handle == nil {
			c.Send(mustEncode(TypeError, ErrorEvent{Code: "unknown_type", Message: "unsupported message type: " + env.Type}))
			continue
		}
		handle(c, env)
	}
}

// mustEncode encodes a known-good payload type (a plain struct with no
// unmarshalable fields), for which Encode cannot fail.
func mustEncode(msgType string, data any) []byte {
	raw, err := Encode(msgType, data)
	if err != nil {
		panic(err)
	}
	return raw
}

// Send queues payload on c.send, non-blocking — a client too backed
// up to receive is already being evicted by the room.
func (c *Client) Send(payload []byte) {
	select {
	case c.send <- outbound{payload: payload, enqueued: time.Now()}:
	default:
	}
}

// WritePump drains c.send, writing each payload as a text message, and
// sends a heartbeat ping on cfg.PingInterval. It returns — closing the
// connection — when send is closed or a write fails. On a successful
// write it observes the enqueue-to-write latency on c.sync, when set —
// this is what measures spec §7's WebSocket sync target, including time
// spent waiting in this client's send buffer, not just the write call.
func (c *Client) WritePump() {
	defer c.conn.Close()

	ticker := time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case o, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(c.cfg.WriteWait))
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, o.payload); err != nil {
				return
			}
			if c.sync != nil {
				c.sync.Observe(time.Since(o.enqueued))
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.cfg.WriteWait)); err != nil {
				return
			}
		}
	}
}
