package ws

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var errBrokenPipe = errors.New("broken pipe")

// stubConn is a test double for Conn. It records written messages and
// control frames on its own storage, guarded by a mutex since the
// pump writes from its own goroutine while the test reads from
// another — this is the one place in this package where a mutex is
// correct, since it is test-local state, not room or hub state.
type stubConn struct {
	mu sync.Mutex

	writeMessages     []writeMessageCall
	writeControls     []writeControlCall
	setWriteDeadlines []time.Time
	setReadDeadlines  []time.Time
	setReadLimits     []int64
	pongHandler       func(string) error
	closed            int

	writeMessageErr func(callIndex int) error
	readMessageFunc func() (int, []byte, error)
}

type writeMessageCall struct {
	messageType int
	data        []byte
}

type writeControlCall struct {
	messageType int
	data        []byte
	deadline    time.Time
}

func newStubConn() *stubConn {
	return &stubConn{}
}

func (s *stubConn) ReadMessage() (int, []byte, error) {
	if s.readMessageFunc != nil {
		return s.readMessageFunc()
	}
	return websocket.TextMessage, nil, nil
}

func (s *stubConn) WriteMessage(messageType int, data []byte) error {
	s.mu.Lock()
	idx := len(s.writeMessages)
	s.writeMessages = append(s.writeMessages, writeMessageCall{messageType: messageType, data: data})
	errFn := s.writeMessageErr
	s.mu.Unlock()
	if errFn != nil {
		return errFn(idx)
	}
	return nil
}

func (s *stubConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeControls = append(s.writeControls, writeControlCall{messageType: messageType, data: data, deadline: deadline})
	return nil
}

func (s *stubConn) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setReadDeadlines = append(s.setReadDeadlines, t)
	return nil
}

func (s *stubConn) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setWriteDeadlines = append(s.setWriteDeadlines, t)
	return nil
}

func (s *stubConn) SetReadLimit(limit int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setReadLimits = append(s.setReadLimits, limit)
}

func (s *stubConn) SetPongHandler(h func(string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pongHandler = h
}

func (s *stubConn) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

func (s *stubConn) WriteMessages() []writeMessageCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]writeMessageCall, len(s.writeMessages))
	copy(out, s.writeMessages)
	return out
}

func (s *stubConn) WriteControls() []writeControlCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]writeControlCall, len(s.writeControls))
	copy(out, s.writeControls)
	return out
}

func (s *stubConn) SetWriteDeadlines() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]time.Time, len(s.setWriteDeadlines))
	copy(out, s.setWriteDeadlines)
	return out
}

func (s *stubConn) Closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func TestWritePump(t *testing.T) {
	// Arrange
	stub := newStubConn()
	cfg := DefaultClientConfig()
	cfg.SendBuffer = 4
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)

	// Act
	go c.WritePump()
	c.send <- []byte("a")
	c.send <- []byte("b")
	waitFor(t, func() bool { return len(stub.WriteMessages()) >= 2 })

	// Assert
	msgs := stub.WriteMessages()
	if len(msgs) != 2 {
		t.Fatalf("got %d WriteMessage calls, want 2", len(msgs))
	}
	if msgs[0].messageType != websocket.TextMessage || string(msgs[0].data) != "a" {
		t.Errorf("msgs[0] = %+v, want TextMessage \"a\"", msgs[0])
	}
	if msgs[1].messageType != websocket.TextMessage || string(msgs[1].data) != "b" {
		t.Errorf("msgs[1] = %+v, want TextMessage \"b\"", msgs[1])
	}
	deadlines := stub.SetWriteDeadlines()
	if len(deadlines) < 2 {
		t.Fatalf("got %d SetWriteDeadline calls, want at least 2", len(deadlines))
	}
	for i, d := range deadlines[:2] {
		if !d.After(time.Now().Add(-time.Second)) {
			t.Errorf("deadline[%d] = %v, want a deadline in the future", i, d)
		}
	}
}

func TestWritePumpPings(t *testing.T) {
	// Arrange
	stub := newStubConn()
	cfg := DefaultClientConfig()
	cfg.PingInterval = 20 * time.Millisecond
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)

	// Act
	go c.WritePump()
	waitFor(t, func() bool { return len(stub.WriteControls()) >= 2 })

	// Assert: at least two pings, never an exact count — exact makes this a timing flake
	controls := stub.WriteControls()
	pingCount := 0
	for _, ctl := range controls {
		if ctl.messageType == websocket.PingMessage {
			pingCount++
			if !ctl.deadline.After(time.Now().Add(-time.Second)) {
				t.Errorf("ping deadline = %v, want a deadline in the future", ctl.deadline)
			}
		}
	}
	if pingCount < 2 {
		t.Fatalf("got %d ping control frames, want at least 2", pingCount)
	}
}

func TestWritePumpClosesConn(t *testing.T) {
	// Arrange
	stub := newStubConn()
	cfg := DefaultClientConfig()
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)
	done := make(chan struct{})

	// Act
	go func() {
		c.WritePump()
		close(done)
	}()
	close(c.send)

	// Assert
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("WritePump did not return after send was closed")
	}
	if got := stub.Closed(); got != 1 {
		t.Fatalf("Close() called %d times, want 1", got)
	}
	if got := len(stub.WriteMessages()); got != 0 {
		t.Fatalf("got %d WriteMessage calls after close, want 0", got)
	}
}

func TestWritePumpWriteError(t *testing.T) {
	// Arrange
	stub := newStubConn()
	stub.writeMessageErr = func(callIndex int) error {
		if callIndex == 0 {
			return errBrokenPipe
		}
		return nil
	}
	cfg := DefaultClientConfig()
	cfg.SendBuffer = 4
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)
	done := make(chan struct{})

	// Act
	go func() {
		c.WritePump()
		close(done)
	}()
	c.send <- []byte("a")

	// Assert
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("WritePump did not return after write failure")
	}
	if got := stub.Closed(); got != 1 {
		t.Fatalf("Close() called %d times, want 1", got)
	}

	// Act: push a second payload after the pump has stopped
	select {
	case c.send <- []byte("b"):
	default:
	}

	// Assert: no second WriteMessage was ever recorded
	time.Sleep(20 * time.Millisecond)
	if got := len(stub.WriteMessages()); got != 1 {
		t.Fatalf("got %d WriteMessage calls, want 1", got)
	}
}

func TestReadPumpDispatch(t *testing.T) {
	// Arrange
	stub := newStubConn()
	msg := []byte(`{"type":"place_wager","data":{"amount":50}}`)
	delivered := false
	blockCh := make(chan struct{})
	stub.readMessageFunc = func() (int, []byte, error) {
		if !delivered {
			delivered = true
			return websocket.TextMessage, msg, nil
		}
		<-blockCh
		return 0, nil, io.EOF
	}
	cfg := DefaultClientConfig()
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)

	type call struct {
		c   *Client
		env Envelope
	}
	calls := make(chan call, 4)
	handler := func(client *Client, env Envelope) {
		calls <- call{c: client, env: env}
	}

	// Act
	go c.ReadPump(handler, nil)

	// Assert
	select {
	case got := <-calls:
		if got.c != c {
			t.Errorf("handler called with client %p, want %p", got.c, c)
		}
		if got.env.Type != "place_wager" {
			t.Errorf("Envelope.Type = %q, want %q", got.env.Type, "place_wager")
		}
		var data map[string]any
		if err := json.Unmarshal(got.env.Data, &data); err != nil {
			t.Fatalf("failed to unmarshal Data: %v", err)
		}
		want := map[string]any{"amount": float64(50)}
		if len(data) != len(want) || data["amount"] != want["amount"] {
			t.Errorf("Data = %+v, want %+v", data, want)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handler was not called")
	}

	// Assert: exactly one call
	select {
	case extra := <-calls:
		t.Fatalf("handler called a second time with %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}

	close(blockCh)
}

func TestReadPumpErrorReply(t *testing.T) {
	// Arrange
	stub := newStubConn()
	messages := [][]byte{
		[]byte("{not json"),
		[]byte(`{"type":"nonsense"}`),
	}
	idx := 0
	blockCh := make(chan struct{})
	stub.readMessageFunc = func() (int, []byte, error) {
		if idx < len(messages) {
			m := messages[idx]
			idx++
			return websocket.TextMessage, m, nil
		}
		<-blockCh
		return 0, nil, io.EOF
	}
	cfg := DefaultClientConfig()
	cfg.SendBuffer = 4
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)

	// Act
	go c.ReadPump(nil, nil)

	// Assert: two error replies land on this client's own send channel
	first := readWithTimeout(t, c.send)
	second := readWithTimeout(t, c.send)

	env1, err := Decode(first)
	if err != nil {
		t.Fatalf("Decode(first) error: %v", err)
	}
	if env1.Type != TypeError {
		t.Errorf("first Type = %q, want %q", env1.Type, TypeError)
	}
	var ev1 ErrorEvent
	if err := json.Unmarshal(env1.Data, &ev1); err != nil {
		t.Fatalf("failed to unmarshal first Data: %v", err)
	}
	if ev1.Code != "malformed" || ev1.Message == "" {
		t.Errorf("first ErrorEvent = %+v, want Code %q and non-empty Message", ev1, "malformed")
	}

	env2, err := Decode(second)
	if err != nil {
		t.Fatalf("Decode(second) error: %v", err)
	}
	if env2.Type != TypeError {
		t.Errorf("second Type = %q, want %q", env2.Type, TypeError)
	}
	var ev2 ErrorEvent
	if err := json.Unmarshal(env2.Data, &ev2); err != nil {
		t.Fatalf("failed to unmarshal second Data: %v", err)
	}
	if ev2.Code != "unknown_type" || ev2.Message == "" {
		t.Errorf("second ErrorEvent = %+v, want Code %q and non-empty Message", ev2, "unknown_type")
	}

	close(blockCh)
}

func TestReadPumpPong(t *testing.T) {
	// Arrange
	stub := newStubConn()
	blockCh := make(chan struct{})
	stub.readMessageFunc = func() (int, []byte, error) {
		<-blockCh
		return 0, nil, io.EOF
	}
	cfg := DefaultClientConfig()
	cfg.PongWait = 5 * time.Second
	c := NewClient(stub, Identity{UserID: "u1"}, cfg)

	// Act
	go c.ReadPump(nil, nil)
	waitFor(t, func() bool {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		return len(stub.setReadLimits) >= 1 && len(stub.setReadDeadlines) >= 1 && stub.pongHandler != nil
	})

	// Assert: read limit and initial deadline were set
	stub.mu.Lock()
	limits := append([]int64{}, stub.setReadLimits...)
	deadlinesBefore := len(stub.setReadDeadlines)
	pongHandler := stub.pongHandler
	stub.mu.Unlock()
	if len(limits) != 1 || limits[0] != cfg.MaxMessage {
		t.Fatalf("setReadLimits = %v, want [%d]", limits, cfg.MaxMessage)
	}
	if pongHandler == nil {
		t.Fatal("no pong handler installed")
	}

	// Act: invoke the installed pong handler
	if err := pongHandler(""); err != nil {
		t.Fatalf("pong handler returned error: %v", err)
	}

	// Assert: a further SetReadDeadline was recorded, at least PongWait-1s out
	stub.mu.Lock()
	deadlinesAfter := append([]time.Time{}, stub.setReadDeadlines...)
	stub.mu.Unlock()
	if len(deadlinesAfter) <= deadlinesBefore {
		t.Fatalf("got %d SetReadDeadline calls after pong, want more than %d", len(deadlinesAfter), deadlinesBefore)
	}
	last := deadlinesAfter[len(deadlinesAfter)-1]
	if !last.After(time.Now().Add(4 * time.Second)) {
		t.Errorf("pong deadline = %v, want at least 4s in the future", last)
	}

	close(blockCh)
}

func readWithTimeout(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for send channel")
		return nil
	}
}

// waitFor polls cond until it's true or a timeout elapses, failing the
// test on timeout. Used instead of a fixed sleep to avoid flaking on a
// loaded runner.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition not met before timeout")
	}
}
