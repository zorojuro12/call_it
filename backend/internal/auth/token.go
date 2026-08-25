package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// Issue signs a token carrying c, with iat/exp/iss set from the
// issuer's clock and TTL.
func (i *Issuer) Issue(c Claims) (string, error) {
	claims := jwt.MapClaims{
		"sub":     c.UserID,
		"name":    c.DisplayName,
		"room_id": c.RoomID,
		"guest":   c.Guest,
		"iss":     Issuer_,
		"iat":     jwt.NewNumericDate(i.now()),
		"exp":     jwt.NewNumericDate(i.now().Add(i.ttl)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(i.secret)
}

// Verify parses and validates a token, returning the Claims it carries.
func (i *Issuer) Verify(tokenString string) (Claims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return i.secret, nil
	}, jwt.WithTimeFunc(i.now), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, fmt.Errorf("%w: %v", ErrTokenExpired, err)
		}
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return Claims{}, ErrInvalidToken
	}

	if iss, ok := claims["iss"].(string); !ok || iss != Issuer_ {
		return Claims{}, ErrInvalidToken
	}

	c := Claims{}
	if v, ok := claims["sub"].(string); ok {
		c.UserID = v
	}
	if v, ok := claims["name"].(string); ok {
		c.DisplayName = v
	}
	if v, ok := claims["room_id"].(string); ok {
		c.RoomID = v
	}
	if v, ok := claims["guest"].(bool); ok {
		c.Guest = v
	}

	return c, nil
}
