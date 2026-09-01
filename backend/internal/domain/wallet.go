package domain

import "fmt"

// ValidateBuyIn rejects a host-configured room buy-in outside the
// platform bounds.
func ValidateBuyIn(buyIn Tokens) error {
	if buyIn < MinBuyIn || buyIn > MaxBuyIn {
		return fmt.Errorf("%w: %d not in %d-%d", ErrInvalidBuyIn, buyIn, MinBuyIn, MaxBuyIn)
	}
	return nil
}

// GuestSessionBalance is what a guest joins a room with: exactly the
// room's buy-in, wiped when the session ends (spec §3). Guests hold no
// persistent account, so the account-holder multiple never applies to
// them.
func GuestSessionBalance(roomBuyIn Tokens) Tokens {
	return roomBuyIn
}

// AccountSessionBalance is what an account holder joins a room with:
// min(StakeCapMultiple x buy-in, account balance) (plan §8). Handing
// them the whole cap up front is what lets place_wager.lua check nothing
// but the session balance — the cap is embodied in the wallet rather
// than re-evaluated on every wager.
func AccountSessionBalance(accountBalance, roomBuyIn Tokens) Tokens {
	limit := roomBuyIn * StakeCapMultiple
	if accountBalance < limit {
		return accountBalance
	}
	return limit
}

// IsPartialBuyIn reports whether an account holder is joining with less
// than the room's full buy-in, which the UI surfaces transparently
// (spec §3, e.g. "joined with 200/2000"). It is a display rule, not a
// gate — a partial buy-in is always permitted.
func IsPartialBuyIn(accountBalance, roomBuyIn Tokens) bool {
	return accountBalance < roomBuyIn
}

// ValidateStakeAmount rejects a wager that is not a positive whole
// number of tokens, independent of any balance. A zero stake is not a
// wager; a negative one would mint tokens out of the pool.
func ValidateStakeAmount(amount Tokens) error {
	if amount <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidStake, amount)
	}
	return nil
}

// ValidateStake rejects a wager that is not a positive whole number of
// tokens, or that exceeds the wallet it would be drawn from. The 3x
// account cap needs no check here — it is already embodied in
// sessionBalance by AccountSessionBalance.
func ValidateStake(amount, sessionBalance Tokens) error {
	if err := ValidateStakeAmount(amount); err != nil {
		return err
	}
	if amount > sessionBalance {
		return fmt.Errorf("%w: stake %d, balance %d", ErrInsufficientFunds, amount, sessionBalance)
	}
	return nil
}

// ApplySessionResult folds a finished session's net profit or loss into
// a persistent account balance (spec §3). It is the delta that carries
// across, not the session's final balance — an account holder who brings
// 1,500 of a 10,000 balance into a room and leaves with 2,400 gains 900,
// not 2,400.
//
// The floor at zero is defence in depth: a session balance never exceeds
// the account balance it came from, so the sum cannot legitimately go
// negative. It exists so that an inconsistent caller cannot mint a
// negative account rather than because the arithmetic needs it.
func ApplySessionResult(accountBalance, sessionStart, sessionEnd Tokens) Tokens {
	updated := accountBalance + (sessionEnd - sessionStart)
	if updated < 0 {
		return 0
	}
	return updated
}
