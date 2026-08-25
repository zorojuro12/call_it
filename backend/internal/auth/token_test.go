package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validSecret() []byte {
	return bytes.Repeat([]byte("a"), 32)
}

func TestNewIssuer(t *testing.T) {
	if _, err := NewIssuer(bytes.Repeat([]byte("a"), 31), time.Hour); !errors.Is(err, ErrWeakSecret) {
		t.Errorf("NewIssuer(31 bytes) err = %v, want ErrWeakSecret", err)
	}

	if _, err := NewIssuer(nil, time.Hour); !errors.Is(err, ErrWeakSecret) {
		t.Errorf("NewIssuer(nil) err = %v, want ErrWeakSecret", err)
	}

	issuer, err := NewIssuer(validSecret(), time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer(32 bytes) unexpected error: %v", err)
	}
	if issuer == nil {
		t.Fatal("NewIssuer(32 bytes) returned nil issuer with nil error")
	}

	if _, err := NewIssuer(validSecret(), 0); err == nil {
		t.Error("NewIssuer(ttl=0) expected an error mentioning the TTL, got nil")
	}

	if _, err := NewIssuer(validSecret(), -time.Second); err == nil {
		t.Error("NewIssuer(ttl<0) expected an error mentioning the TTL, got nil")
	}
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	issuer, err := NewIssuer(validSecret(), time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}

	cases := []Claims{
		{UserID: "u1", DisplayName: "Alice", RoomID: "", Guest: false},
		{UserID: "u1", DisplayName: "Alice", RoomID: "r1", Guest: false},
		{UserID: "g1", DisplayName: "Bob", RoomID: "r1", Guest: true},
	}

	for _, want := range cases {
		token, err := issuer.Issue(want)
		if err != nil {
			t.Fatalf("Issue(%+v) unexpected error: %v", want, err)
		}

		segments := strings.Split(token, ".")
		if len(segments) != 3 {
			t.Fatalf("Issue(%+v) token has %d '.'-separated segments, want 3", want, len(segments))
		}

		header, err := base64.RawURLEncoding.DecodeString(segments[0])
		if err != nil {
			t.Fatalf("token header does not decode as base64url: %v", err)
		}
		var headerFields map[string]any
		if err := json.Unmarshal(header, &headerFields); err != nil {
			t.Fatalf("token header does not decode as JSON: %v", err)
		}
		if headerFields["alg"] != "HS256" {
			t.Errorf("token header alg = %v, want HS256", headerFields["alg"])
		}

		got, err := issuer.Verify(token)
		if err != nil {
			t.Fatalf("Verify() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Verify(Issue(%+v)) = %+v, want %+v", want, got, want)
		}
	}
}

func TestVerifyExpired(t *testing.T) {
	issuer, err := NewIssuer(validSecret(), time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issuer.now = func() time.Time { return base }

	token, err := issuer.Issue(Claims{UserID: "u1"})
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	issuer.now = func() time.Time { return base.Add(59 * time.Minute) }
	if _, err := issuer.Verify(token); err != nil {
		t.Errorf("Verify() at T+59m: unexpected error: %v", err)
	}

	issuer.now = func() time.Time { return base.Add(2 * time.Hour) }
	_, err = issuer.Verify(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("Verify() at T+2h: err = %v, want ErrTokenExpired", err)
	}
	if errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify() at T+2h: err = %v, must not also satisfy ErrInvalidToken", err)
	}
}
