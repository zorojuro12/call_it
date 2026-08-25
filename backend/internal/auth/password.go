package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

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
