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
