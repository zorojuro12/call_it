package redisstore

import "errors"

// ErrNotFound is returned when a key or hash field this package expects
// is absent. redis.Nil never escapes this package — every read wraps it
// into this sentinel instead, so callers never need to import go-redis
// to handle a not-found case.
var ErrNotFound = errors.New("redisstore: key or field not found")
