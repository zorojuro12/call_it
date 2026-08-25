package auth

import (
	"fmt"
	"time"
)

// MinSecretLen is the floor on an HMAC signing key: HS256's key should
// be at least as long as the hash output it produces, or the
// signature's effective strength drops below the algorithm's.
const MinSecretLen = 32

// Issuer_ is the token "iss" claim, checked on every Verify so a token
// signed by an unrelated issuer sharing the same secret is still
// rejected.
const Issuer_ = "callit"

// Claims is the identity an issued token carries. RoomID is empty on an
// account-scoped token (registration, login, refill) and set on a
// room-scoped one (created by joining or hosting a room).
type Claims struct {
	UserID      string
	DisplayName string
	RoomID      string
	Guest       bool
}

// Issuer signs and verifies HS256 tokens against one secret and TTL.
type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewIssuer constructs an Issuer, refusing a secret shorter than
// MinSecretLen or a non-positive TTL. config already enforces the same
// 32-byte floor at startup; this enforces it again at the type that
// actually signs, so a future caller constructing an Issuer from
// somewhere other than config cannot weaken it.
func NewIssuer(secret []byte, ttl time.Duration) (*Issuer, error) {
	if len(secret) < MinSecretLen {
		return nil, fmt.Errorf("%w: must be at least %d bytes, got %d", ErrWeakSecret, MinSecretLen, len(secret))
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: token TTL must be positive, got %s", ttl)
	}

	return &Issuer{secret: secret, ttl: ttl, now: time.Now}, nil
}
