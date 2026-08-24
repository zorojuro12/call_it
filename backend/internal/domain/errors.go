package domain

import "errors"

// The domain's failure vocabulary. Callers match with errors.Is and map
// to wire codes at the boundary. This list is deliberately limited to
// failures this package can actually produce — the remaining Lua return
// codes (POOL_LOCKED, HOST_CANNOT_BET, NOT_IN_ROOM) gain Go counterparts
// in Phase 2, when something here returns them.
var (
	ErrInvalidTransition = errors.New("domain: invalid round status transition")
)
