package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestEmail(t *testing.T) {
	t.Run("normalize", func(t *testing.T) {
		cases := map[string]string{
			"  Alice@Example.COM  ": "alice@example.com",
			"alice@example.com":     "alice@example.com",
		}
		for raw, want := range cases {
			if got := NormalizeEmail(raw); got != want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", raw, got, want)
			}
		}
	})

	t.Run("valid", func(t *testing.T) {
		valid := []string{"a@b.co", "alice.smith+tag@example.co.uk"}
		for _, addr := range valid {
			if err := ValidateEmail(addr); err != nil {
				t.Errorf("ValidateEmail(%q) unexpected error: %v", addr, err)
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		invalid := []string{
			"", "nope", "a@", "@b.c", "a@b", "a b@c.d", "a@@b.c",
			"a@b..c", "a@.b.c", "a@b.c.",
			strings.Repeat("a", 251) + "@b.c", // 255 chars total
			strings.Repeat("a", 65) + "@b.co", // 65-char local part
		}
		for _, addr := range invalid {
			if err := ValidateEmail(addr); !errors.Is(err, ErrInvalidEmail) {
				t.Errorf("ValidateEmail(%q) err = %v, want ErrInvalidEmail", addr, err)
			}
		}
	})
}
