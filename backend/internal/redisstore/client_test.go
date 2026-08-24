package redisstore

import "testing"

func TestNew_ConnectionError(t *testing.T) {
	_, err := New("127.0.0.1:1", testDB)
	if err == nil {
		t.Fatalf("New() with an unreachable address = nil error, want an error")
	}
}
