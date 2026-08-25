package room

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func TestGenerateCode(t *testing.T) {
	t.Run("shape", func(t *testing.T) {
		code, err := GenerateCode(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateCode() = %v, want nil", err)
		}
		if len(code) != CodeLen {
			t.Fatalf("len(code) = %d, want %d", len(code), CodeLen)
		}
		for _, c := range code {
			if !strings.ContainsRune(CodeAlphabet, c) {
				t.Errorf("code %q contains %q, not in CodeAlphabet", code, c)
			}
		}
	})

	t.Run("alphabet shape", func(t *testing.T) {
		if len(CodeAlphabet) != 31 {
			t.Errorf("len(CodeAlphabet) = %d, want 31", len(CodeAlphabet))
		}
		for _, bad := range []rune{'0', 'O', '1', 'I', 'L'} {
			if strings.ContainsRune(CodeAlphabet, bad) {
				t.Errorf("CodeAlphabet contains excluded character %q", bad)
			}
		}
		seen := map[rune]bool{}
		for _, c := range CodeAlphabet {
			if seen[c] {
				t.Errorf("CodeAlphabet contains repeated character %q", c)
			}
			seen[c] = true
		}
	})

	t.Run("distinctness", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 1000; i++ {
			code, err := GenerateCode(rand.Reader)
			if err != nil {
				t.Fatalf("GenerateCode() call %d = %v, want nil", i, err)
			}
			seen[code] = true
		}
		if len(seen) < 990 {
			t.Errorf("distinct codes across 1000 calls = %d, want >= 990", len(seen))
		}
	})

	t.Run("short read is an error", func(t *testing.T) {
		if _, err := GenerateCode(bytes.NewReader(nil)); err == nil {
			t.Error("GenerateCode(empty reader) = nil error, want an error")
		}
	})

	t.Run("deterministic mapping", func(t *testing.T) {
		zeros := bytes.Repeat([]byte{0x00}, CodeLen)
		code, err := GenerateCode(bytes.NewReader(zeros))
		if err != nil {
			t.Fatalf("GenerateCode(zeros) = %v, want nil", err)
		}
		want := strings.Repeat(string(CodeAlphabet[0]), CodeLen)
		if code != want {
			t.Errorf("GenerateCode(zeros) = %q, want %q", code, want)
		}
	})
}
