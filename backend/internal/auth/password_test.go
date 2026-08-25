package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	const plain = "correct horse battery staple"

	got1, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword() unexpected error: %v", err)
	}

	const wantPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(got1, wantPrefix) {
		t.Fatalf("HashPassword() = %q, want prefix %q", got1, wantPrefix)
	}

	parts := strings.Split(got1, "$")
	if len(parts) != 6 {
		t.Fatalf("HashPassword() split on '$' has %d parts, want 6: %q", len(parts), got1)
	}

	saltSeg := parts[4]
	hashSeg := parts[5]

	salt, err := base64.RawStdEncoding.DecodeString(saltSeg)
	if err != nil {
		t.Fatalf("salt segment %q does not decode as base64: %v", saltSeg, err)
	}
	if len(salt) != 16 {
		t.Fatalf("salt segment decodes to %d bytes, want 16", len(salt))
	}

	hash, err := base64.RawStdEncoding.DecodeString(hashSeg)
	if err != nil {
		t.Fatalf("hash segment %q does not decode as base64: %v", hashSeg, err)
	}
	if len(hash) != 32 {
		t.Fatalf("hash segment decodes to %d bytes, want 32", len(hash))
	}

	got2, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword() second call unexpected error: %v", err)
	}
	if got1 == got2 {
		t.Fatalf("two HashPassword() calls on the same input produced identical strings; salt is not random")
	}
}
