package round

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// RefundGrace is how long an unresolved round is left locked before it
// auto-refunds every stake — the host-disconnect fallback (spec §4).
const RefundGrace = 60 * time.Second

// watch runs one round's whole server-side clock: lock at lockAt, then
// (from Task 7) auto-refund refundGrace later if still unresolved.
// Started by Open in its own goroutine against a background context,
// not the opening request's — a disconnecting host must not cancel the
// round's clock.
func (s *Service) watch(ctx context.Context, roomID, roundID string, lockAt time.Time) {
	<-time.After(time.Until(lockAt))

	err := s.store.LockRound(ctx, roundID)
	if errors.Is(err, redisstore.ErrRoundTerminal) {
		// A round resolved before its lock instant needs no lock —
		// this is a benign race between the timer and a fast host,
		// not a failure. Also covers this checkpoint's case: a round
		// that resolved before the timer ever fired.
		return
	}
	if err != nil {
		log.Printf("round: lock round %s: %v", roundID, err)
		return
	}

	payload, err := EncodeEnvelope("round_locked", LockedEvent{RoundID: roundID})
	if err != nil {
		log.Printf("round: encode round_locked for %s: %v", roundID, err)
		return
	}
	s.broadcaster.Broadcast(roomID, payload)
}
