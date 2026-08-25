// Package auth holds pure identity primitives — argon2id password
// hashing, credential validation, and JWT issue/verify — with no I/O, so
// it unit-tests with nothing running.
package auth

import "errors"

var (
	ErrPasswordMismatch   = errors.New("auth: password does not match")
	ErrMalformedHash      = errors.New("auth: malformed password hash")
	ErrInvalidEmail       = errors.New("auth: invalid email address")
	ErrWeakPassword       = errors.New("auth: password does not meet requirements")
	ErrInvalidDisplayName = errors.New("auth: invalid display name")
)
