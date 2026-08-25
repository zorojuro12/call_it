package room

import "io"

// CodeLen is the number of characters in a generated room code.
const CodeLen = 6

// CodeAlphabet omits 0/O and 1/I/L: codes get read aloud across a room
// and typed from a glance at someone's screen, so visually confusable
// characters turn into failed joins.
const CodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateCode reads CodeLen bytes from r and maps each to a character
// in CodeAlphabet.
//
// The modulo introduces a slight bias toward the first 256%31=8
// characters. That is acceptable here and deliberately not corrected:
// the code is a short-lived lookup handle for a room whose participants
// were invited out of band, not a secret. Room authorization rests on
// the JWT, not on code unguessability.
func GenerateCode(r io.Reader) (string, error) {
	buf := make([]byte, CodeLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}

	code := make([]byte, CodeLen)
	for i, b := range buf {
		code[i] = CodeAlphabet[int(b)%len(CodeAlphabet)]
	}

	return string(code), nil
}
