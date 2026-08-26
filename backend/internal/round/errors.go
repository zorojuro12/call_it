package round

import "errors"

// ErrNotHost is returned when a non-host caller attempts to control a
// round — opening or resolving it. Only the room's host may do either.
var ErrNotHost = errors.New("round: only the room host may control rounds")

// ErrRoundInProgress is returned when a second round is opened while
// one is already open in the room.
var ErrRoundInProgress = errors.New("round: a round is already open in this room")

// ErrInvalidSpec is returned when a round's question, outcomes, or lock
// window fail validation.
var ErrInvalidSpec = errors.New("round: round specification is invalid")

// ErrNoActiveRound is returned when an action needs a room's current
// round but the room has none.
var ErrNoActiveRound = errors.New("round: the room has no active round")
