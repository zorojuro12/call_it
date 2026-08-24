package redisstore

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a key or hash field this package expects
// is absent. redis.Nil never escapes this package — every read wraps it
// into this sentinel instead, so callers never need to import go-redis
// to handle a not-found case.
var ErrNotFound = errors.New("redisstore: key or field not found")

// mapWagerStatus maps place_wager.lua's non-OK status codes to Go
// errors. Task 4 adds a case per guard as each one is implemented; an
// unrecognized code for now returns a wrapped generic error naming it,
// since no guard has been added yet.
func mapWagerStatus(reply []string) error {
	code := reply[0]
	switch code {
	default:
		return fmt.Errorf("redisstore: place wager: unrecognized status %q", code)
	}
}
