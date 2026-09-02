package round

import (
	"log"
	"time"
)

// SessionGrace is how long a disconnected player's session survives
// before it is folded. Long enough to cover a browser refresh plus a
// fresh WebSocket handshake (sub-second in practice) and a short
// network blip; short enough to sit well under RefundGrace's 60
// seconds, so a disconnected player's session never outlives the
// round-level fallback that would refund their stake anyway.
const SessionGrace = 30 * time.Second

// sessionKey identifies one room/user pair's pending session end. A NUL
// separator means no ID content can forge a collision between two
// distinct pairs.
func sessionKey(roomID, userID string) string {
	return roomID + "\x00" + userID
}

// ScheduleEndSession starts (or restarts) a grace window after which
// userID's session in roomID is folded via EndSession, unless
// ResumeSession cancels it first. Returns immediately; the fold, if it
// happens, runs against the Service's base context, never the caller's
// — a fold must not be cancelled by the disconnect that scheduled it.
//
// A guest is never scheduled (a guest has no persistent balance to fold
// into, so EndSession is already a no-op for one) — burning a goroutine
// and a timer to reach a no-op 30 seconds later would be pure waste.
func (s *Service) ScheduleEndSession(roomID, userID string, guest bool) {
	if guest {
		return
	}

	key := sessionKey(roomID, userID)
	done := make(chan struct{})

	s.mu.Lock()
	if prev, ok := s.pending[key]; ok {
		close(prev)
	}
	s.pending[key] = done
	s.mu.Unlock()

	go func() {
		timer := time.NewTimer(s.sessionGrace)
		defer timer.Stop()

		select {
		case <-done:
			return
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}

		// The identity re-check under the lock is what makes a
		// schedule→resume→schedule sequence safe: without it a stale
		// goroutine whose window already elapsed could fold a session
		// a live connection is still using, racing a replacement
		// goroutine that owns the pending entry now.
		s.mu.Lock()
		current, ok := s.pending[key]
		if !ok || current != done {
			s.mu.Unlock()
			return
		}
		delete(s.pending, key)
		s.mu.Unlock()

		if _, err := s.EndSession(s.ctx, roomID, userID, false); err != nil {
			log.Printf("round: end session for %s in room %s: %v", userID, roomID, err)
		}
	}()
}

// ResumeSession cancels a pending session end for roomID/userID if one
// exists; a no-op otherwise. Nothing in the system branches on whether
// an end was actually pending, so this has no return value — safe to
// call unconditionally on every connect, including a guest's and an
// ordinary first connect that was never preceded by a disconnect.
func (s *Service) ResumeSession(roomID, userID string) {
	key := sessionKey(roomID, userID)

	s.mu.Lock()
	defer s.mu.Unlock()

	done, ok := s.pending[key]
	if !ok {
		return
	}
	close(done)
	delete(s.pending, key)
}
