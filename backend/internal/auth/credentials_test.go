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

func TestValidatePassword(t *testing.T) {
	valid := []string{strings.Repeat("a", 12), strings.Repeat("a", 128)}
	for _, pw := range valid {
		if err := ValidatePassword(pw); err != nil {
			t.Errorf("ValidatePassword(len=%d) unexpected error: %v", len(pw), err)
		}
	}

	invalid := []string{"", strings.Repeat("a", 11), strings.Repeat("a", 129)}
	for _, pw := range invalid {
		err := ValidatePassword(pw)
		if !errors.Is(err, ErrWeakPassword) {
			t.Errorf("ValidatePassword(len=%d) err = %v, want ErrWeakPassword", len(pw), err)
		}
		if err != nil && !strings.Contains(err.Error(), "12") {
			t.Errorf("ValidatePassword(len=%d) error %q does not name the permitted range", len(pw), err.Error())
		}
	}
}

func TestDisplayName(t *testing.T) {
	t.Run("normalize", func(t *testing.T) {
		if got := NormalizeDisplayName("  Alice  "); got != "Alice" {
			t.Errorf("NormalizeDisplayName(%q) = %q, want %q", "  Alice  ", got, "Alice")
		}
	})

	t.Run("valid", func(t *testing.T) {
		valid := []string{"Alice", "J", strings.Repeat("a", 32), "あかり"}
		for _, name := range valid {
			if err := ValidateDisplayName(name); err != nil {
				t.Errorf("ValidateDisplayName(%q) unexpected error: %v", name, err)
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		invalid := []string{"", strings.Repeat("a", 33), "Alice\nBob", "Alice\x00"}
		for _, name := range invalid {
			if err := ValidateDisplayName(name); !errors.Is(err, ErrInvalidDisplayName) {
				t.Errorf("ValidateDisplayName(%q) err = %v, want ErrInvalidDisplayName", name, err)
			}
		}
	})
}
