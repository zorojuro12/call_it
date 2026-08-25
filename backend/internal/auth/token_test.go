package auth

import (
	"bytes"
	"errors"
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
