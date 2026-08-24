package redisstore

import (
	"errors"
	"testing"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

func TestParseWagerReply_Malformed(t *testing.T) {
	tests := []struct {
		name  string
		reply []string
	}{
		{"too short", []string{"OK", "300"}},
		{"malformed balance", []string{"OK", "not-a-number", "1", "200"}},
		{"malformed bettor count", []string{"OK", "300", "not-a-number", "200"}},
		{"malformed total", []string{"OK", "300", "1", "not-a-number"}},
		{"malformed pool", []string{"OK", "300", "1", "200", "not-a-number"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseWagerReply(tt.reply); err == nil {
				t.Errorf("parseWagerReply(%v) = nil error, want an error", tt.reply)
			}
		})
	}
}

func TestToStringSlice_Malformed(t *testing.T) {
	tests := []struct {
		name string
		res  interface{}
	}{
		{"not an array", "OK"},
		{"element not a string", []interface{}{"OK", 42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := toStringSlice(tt.res); err == nil {
				t.Errorf("toStringSlice(%v) = nil error, want an error", tt.res)
			}
		})
	}
}

func TestMapWagerStatus_Unrecognized(t *testing.T) {
	err := mapWagerStatus([]string{"SOME_UNRECOGNIZED_CODE"})
	if err == nil {
		t.Fatalf("mapWagerStatus(unrecognized) = nil error, want an error")
	}
	for _, sentinel := range []error{ErrPoolLocked, domain.ErrInvalidOutcome, ErrHostCannotBet, ErrNotInRoom} {
		if errors.Is(err, sentinel) {
			t.Errorf("mapWagerStatus(unrecognized) matched %v, want a generic error naming the code", sentinel)
		}
	}
}

func TestMapSettleStatus_Unrecognized(t *testing.T) {
	err := mapSettleStatus([]string{"SOME_UNRECOGNIZED_CODE"})
	if err == nil {
		t.Fatalf("mapSettleStatus(unrecognized) = nil error, want an error")
	}
	if errors.Is(err, ErrAlreadySettled) || errors.Is(err, ErrNotLocked) {
		t.Errorf("mapSettleStatus(unrecognized) matched a known sentinel, want a generic error naming the code")
	}
}
