package domain

import "fmt"

// RoundStatus is a round's lifecycle state. The values are the exact
// strings persisted in the round:{roundID} Redis hash (plan §4), so no
// mapping layer is needed between the domain and the store.
type RoundStatus string

const (
	RoundOpen     RoundStatus = "open"
	RoundLocked   RoundStatus = "locked"
	RoundResolved RoundStatus = "resolved"
	RoundRefunded RoundStatus = "refunded"
)

// validTransitions is the whole state machine. A status absent from this
// map has nowhere legal to go, which is what makes it terminal.
var validTransitions = map[RoundStatus][]RoundStatus{
	RoundOpen:   {RoundLocked},
	RoundLocked: {RoundResolved, RoundRefunded},
}

// Transition returns the status to move to, or an error wrapping
// ErrInvalidTransition if the move is illegal. It never mutates the
// receiver; on failure it returns the unchanged current status.
func (s RoundStatus) Transition(next RoundStatus) (RoundStatus, error) {
	for _, allowed := range validTransitions[s] {
		if allowed == next {
			return next, nil
		}
	}
	return s, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, s, next)
}

// IsTerminal reports whether no further transition is legal from s. That
// covers resolved and refunded rounds, and equally any unrecognized
// status, which by definition has no legal move.
func (s RoundStatus) IsTerminal() bool {
	return len(validTransitions[s]) == 0
}

// AcceptsWagers reports whether a round in this status may take new
// wagers. Only an open round does. This is the status half of the rule
// only — lockout is additionally enforced against the Redis clock inside
// place_wager.lua (plan §5), never against a Go timestamp.
func (s RoundStatus) AcceptsWagers() bool {
	return s == RoundOpen
}
