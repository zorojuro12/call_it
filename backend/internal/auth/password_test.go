package auth

import (
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixtureHash is a package-level fixture: argon2id is deliberately slow,
// so tests that only need *a* valid hash reuse this rather than calling
// HashPassword per case.
var fixtureHash string

func init() {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		panic(err)
	}
	fixtureHash = h
}

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

func TestVerifyPassword(t *testing.T) {
	if err := VerifyPassword(fixtureHash, "correct horse battery staple"); err != nil {
		t.Errorf("VerifyPassword() with correct password: unexpected error: %v", err)
	}

	if err := VerifyPassword(fixtureHash, "Correct horse battery staple"); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("VerifyPassword() with wrong case: err = %v, want ErrPasswordMismatch", err)
	}

	if err := VerifyPassword(fixtureHash, ""); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("VerifyPassword() with empty password: err = %v, want ErrPasswordMismatch", err)
	}

	secondHash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() unexpected error: %v", err)
	}
	if err := VerifyPassword(secondHash, "correct horse battery staple"); err != nil {
		t.Errorf("VerifyPassword() against a second hash of the same password: unexpected error: %v", err)
	}
}

func TestVerifyPassword_Malformed(t *testing.T) {
	cases := []string{
		"",
		"notahash",
		"$argon2id$v=19$m=19456,t=2,p=1$onlyfourparts",
		"$argon2i$v=19$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY",
		"$argon2id$v=16$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY",
		"$argon2id$v=19$m=notanumber,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!notbase64!!!$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY",
	}

	for _, encoded := range cases {
		t.Run(encoded, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("VerifyPassword(%q, ...) panicked: %v", encoded, r)
				}
			}()

			err := VerifyPassword(encoded, "anything")
			if !errors.Is(err, ErrMalformedHash) {
				t.Errorf("VerifyPassword(%q, ...) err = %v, want ErrMalformedHash", encoded, err)
			}
			if errors.Is(err, ErrPasswordMismatch) {
				t.Errorf("VerifyPassword(%q, ...) err = %v, must not also satisfy ErrPasswordMismatch", encoded, err)
			}
		})
	}
}

// median returns the middle value of samples after sorting; samples must
// have odd length for this to be unambiguous, which every caller here
// satisfies with 5 samples.
func median(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func TestVerifyDecoyPasswordCostsAFullVerify(t *testing.T) {
	// VerifyDecoyPassword must compile and return nothing.
	VerifyDecoyPassword("anything")

	// The decoy hash must be a well-formed PHC string this package can
	// parse under its current parameters: VerifyPassword against it must
	// return ErrPasswordMismatch, never ErrMalformedHash.
	if err := VerifyPassword(decoyHash(), "x"); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("VerifyPassword(decoyHash(), ...) err = %v, want ErrPasswordMismatch", err)
	}

	const samples = 5

	decoyTimes := make([]time.Duration, samples)
	for i := range decoyTimes {
		start := time.Now()
		VerifyDecoyPassword("anything")
		decoyTimes[i] = time.Since(start)
	}

	realTimes := make([]time.Duration, samples)
	for i := range realTimes {
		start := time.Now()
		if err := VerifyPassword(fixtureHash, "wrong"); !errors.Is(err, ErrPasswordMismatch) {
			t.Fatalf("VerifyPassword(fixtureHash, \"wrong\") err = %v, want ErrPasswordMismatch", err)
		}
		realTimes[i] = time.Since(start)
	}

	medianDecoy := median(decoyTimes)
	medianReal := median(realTimes)

	if medianDecoy < medianReal/2 {
		t.Errorf("median decoy verify = %v, median real verify = %v; decoy is less than half the real cost", medianDecoy, medianReal)
	}
}
