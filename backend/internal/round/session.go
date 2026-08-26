package round

import (
	"context"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// EndSession folds a departing account holder's net session result into
// their persistent balance. A guest has no persistent balance and is a
// no-op returning (0, nil) — spec §3's "net profit/loss, not final
// balance" is the whole point: domain.ApplySessionResult is the only
// thing permitted to compute the delta.
//
// Known limitation: this fires on socket disconnect, so a player who
// drops and reconnects ends their session and starts a new one at the
// room's buy-in. Reconnect-with-session-resume is deferred to Phase 7
// hardening — it needs a grace window this phase does not have.
func (s *Service) EndSession(ctx context.Context, roomID, userID string, guest bool) (domain.Tokens, error) {
	if guest {
		return 0, nil
	}

	acct, err := s.store.User(ctx, userID)
	if err != nil {
		return 0, err
	}
	opening, err := s.store.OpeningStake(ctx, roomID, userID)
	if err != nil {
		return 0, err
	}
	current, err := s.store.Balance(ctx, roomID, userID)
	if err != nil {
		return 0, err
	}

	newBalance := domain.ApplySessionResult(acct.Balance, opening, current)

	if err := s.store.SetBalance(ctx, userID, newBalance); err != nil {
		return 0, err
	}

	return newBalance, nil
}
