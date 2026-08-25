package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-recommended argon2id parameters for a 19 MiB memory budget.
// Encoded into the stored PHC string, not just applied, so raising them
// later still verifies every hash written under the old ones.
const (
	argon2Time    = 2
	argon2Memory  = 19456
	argon2Threads = 1
	argon2KeyLen  = 32
	saltLen       = 16
)

// HashPassword derives an argon2id key from plain with a fresh random
// salt and returns it PHC-encoded:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
func HashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plain), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword re-derives a key from plain using the parameters and
// salt embedded in encoded, and compares it against the embedded hash in
// constant time.
//
// A parse failure here returns ErrPasswordMismatch, not
// ErrMalformedHash — distinguishing the two is Checkpoint 3's job.
func VerifyPassword(encoded, plain string) error {
	p, salt, wantHash, err := parsePHC(encoded)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedHash, err)
	}

	gotHash := argon2.IDKey([]byte(plain), salt, p.time, p.memory, p.threads, uint32(len(wantHash)))

	if subtle.ConstantTimeCompare(gotHash, wantHash) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// phcParams is the parsed parameter triple from a PHC-encoded hash.
type phcParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// parsePHC splits and validates a PHC-encoded argon2id hash string,
// returning its parameters, salt, and hash. It never re-derives a key.
func parsePHC(encoded string) (phcParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return phcParams{}, nil, nil, fmt.Errorf("expected 6 '$'-separated parts, got %d", len(parts))
	}

	if parts[1] != "argon2id" {
		return phcParams{}, nil, nil, fmt.Errorf("unsupported algorithm %q", parts[1])
	}
	if parts[2] != "v=19" {
		return phcParams{}, nil, nil, fmt.Errorf("unsupported version %q", parts[2])
	}

	var p phcParams
	n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads)
	if err != nil || n != 3 {
		return phcParams{}, nil, nil, fmt.Errorf("malformed parameter segment %q: %w", parts[3], err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("malformed salt segment: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("malformed hash segment: %w", err)
	}

	return p, salt, hash, nil
}
