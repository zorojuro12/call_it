package round

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// RefundGrace is how long an unresolved round is left locked before it
// auto-refunds every stake — the host-disconnect fallback (spec §4).
const RefundGrace = 60 * time.Second

// watch runs one round's whole server-side clock: lock at lockAt, then
// auto-refund refundGrace later if still unresolved. Started by Open in
// its own goroutine against the Service's base context, not the
// opening request's — a disconnecting host must not cancel the round's
// clock. Canceling that base context (e.g. on process shutdown) does
// stop it, at either wait.
func (s *Service) watch(ctx context.Context, roomID, roundID string, lockAt time.Time) {
	lockTimer := time.NewTimer(time.Until(lockAt))
	defer lockTimer.Stop()
	select {
	case <-lockTimer.C:
	case <-ctx.Done():
		return
	}

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

	refundTimer := time.NewTimer(s.refundGrace)
	defer refundTimer.Stop()
	select {
	case <-refundTimer.C:
	case <-ctx.Done():
		return
	}

	rd, err := s.store.Round(ctx, roundID)
	if err != nil {
		log.Printf("round: re-read round %s before refund: %v", roundID, err)
		return
	}
	if rd.Status.IsTerminal() {
		// The host resolved it during the grace window — nothing to
		// refund.
		return
	}

	total, err := s.store.RefundRound(ctx, roundID, uuid.NewString())
	if errors.Is(err, redisstore.ErrAlreadySettled) {
		// Belt and braces: the round could resolve in the window
		// between the status re-read above and this call — a race the
		// re-read alone cannot close.
		return
	}
	if err != nil {
		log.Printf("round: refund round %s: %v", roundID, err)
		return
	}

	if err := s.store.ClearCurrentRound(ctx, roomID); err != nil {
		log.Printf("round: clear current round for %s after refund: %v", roomID, err)
		return
	}

	refundPayload, err := EncodeEnvelope("round_refunded", RefundedEvent{RoundID: roundID, Total: int64(total)})
	if err != nil {
		log.Printf("round: encode round_refunded for %s: %v", roundID, err)
		return
	}
	s.broadcaster.Broadcast(roomID, refundPayload)
}
