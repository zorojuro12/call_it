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
// The fold is once-only by claim: it reads the session's state, then
// atomically claims it via store.ClearSession before crediting anything.
// If the claim reports the session was already gone — a concurrent or
// prior EndSession call already folded it — this returns (0, nil)
// rather than an error or a second credit. Claim-then-credit is
// deliberate: a crash between the two loses a session result, where the
// reverse order (credit-then-claim) would mint tokens on a retry, and
// this codebase's invariants forbid minting. An unknown *user* (no
// account for userID at all) still returns an error unchanged — that is
// a genuine bug signal, not a double-fold, and is pinned by
// session_test.go's never-joined case. Only a missing *opening stake or
// wallet* for a known user — no live session here — collapses to the
// no-op.
//
// A rejoin after a fold starts a genuinely new session at the room
// buy-in, since the fold clears both the wallet and opening-stake
// fields with it. One race is accepted rather than closed: the wallet
// is read here before the claim runs, so a wager landing in between is
// not folded — reachable only if a second live socket for the same user
// places a wager during the disconnect grace window, and closing it
// would mean moving the whole fold into Lua for a case the grace window
// already makes vanishingly rare.
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

	claimed, err := s.store.ClearSession(ctx, roomID, userID)
	if err != nil {
		return 0, err
	}
	if !claimed {
		return 0, nil
	}

	newBalance := domain.ApplySessionResult(acct.Balance, opening, current)

	if err := s.store.SetBalance(ctx, userID, newBalance); err != nil {
		return 0, err
	}

	return newBalance, nil
}
