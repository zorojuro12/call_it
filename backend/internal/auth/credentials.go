package auth

import "strings"

// NormalizeEmail lowercases and trims raw so that the same address
// always maps to the same email:{normalizedEmail} lookup key.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidateEmail applies a hand-rolled rule set to an already-normalized
// address, deliberately narrower than net/mail.ParseAddress, which
// accepts display-name forms like "Alice <a@b.c>" that must never
// become a lookup key.
func ValidateEmail(normalized string) error {
	if len(normalized) < 3 || len(normalized) > 254 {
		return ErrInvalidEmail
	}

	at := strings.IndexByte(normalized, '@')
	if at < 0 || strings.IndexByte(normalized[at+1:], '@') >= 0 {
		return ErrInvalidEmail
	}

	local := normalized[:at]
	domain := normalized[at+1:]

	if len(local) < 1 || len(local) > 64 || strings.ContainsAny(local, " \t") {
		return ErrInvalidEmail
	}

	if !strings.Contains(domain, ".") {
		return ErrInvalidEmail
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return ErrInvalidEmail
	}
	if strings.Contains(domain, "..") {
		return ErrInvalidEmail
	}
	if strings.ContainsAny(domain, " \t") {
		return ErrInvalidEmail
	}

	for _, label := range strings.Split(domain, ".") {
		if len(label) < 1 || len(label) > 63 {
			return ErrInvalidEmail
		}
	}

	return nil
}
