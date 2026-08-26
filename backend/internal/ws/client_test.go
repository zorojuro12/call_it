package ws

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

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
