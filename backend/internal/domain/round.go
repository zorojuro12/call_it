package domain

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

// Transition returns the status to move to, or ErrInvalidTransition if
// the move is illegal. It never mutates the receiver; on failure it
// returns the unchanged current status.
func (s RoundStatus) Transition(next RoundStatus) (RoundStatus, error) {
	for _, allowed := range validTransitions[s] {
		if allowed == next {
			return next, nil
		}
	}
	return s, ErrInvalidTransition
}
