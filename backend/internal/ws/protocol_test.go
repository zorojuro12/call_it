package ws

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Run("connected event", func(t *testing.T) {
		// Arrange
		want := ConnectedEvent{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false}

		// Act
		raw, err := Encode(TypeConnected, want)
		if err != nil {
			t.Fatalf("Encode returned error: %v", err)
		}

		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatalf("Encode output is not valid JSON: %v", err)
		}
		if len(top) != 2 {
			t.Fatalf("expected exactly 2 top-level keys, got %d: %v", len(top), top)
		}
		if _, ok := top["type"]; !ok {
			t.Fatalf("missing top-level key %q", "type")
		}
		if _, ok := top["data"]; !ok {
			t.Fatalf("missing top-level key %q", "data")
		}

		env, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}

		// Assert
		if env.Type != TypeConnected {
			t.Errorf("Type = %q, want %q", env.Type, TypeConnected)
		}
		var got ConnectedEvent
		if err := json.Unmarshal(env.Data, &got); err != nil {
			t.Fatalf("failed to unmarshal Data: %v", err)
		}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("error event", func(t *testing.T) {
		// Arrange
		want := ErrorEvent{Code: "unknown_type", Message: "x"}

		// Act
		raw, err := Encode(TypeError, want)
		if err != nil {
			t.Fatalf("Encode returned error: %v", err)
		}
		env, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}

		// Assert
		if env.Type != TypeError {
			t.Errorf("Type = %q, want %q", env.Type, TypeError)
		}
		var got ErrorEvent
		if err := json.Unmarshal(env.Data, &got); err != nil {
			t.Fatalf("failed to unmarshal Data: %v", err)
		}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

func TestDecodeRejectsMalformed(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{"not json", []byte("{not json"), ErrMalformed},
		{"json array", []byte("[]"), ErrMalformed},
		{"missing type", []byte(`{"data":{}}`), ErrMissingType},
		{"empty type", []byte(`{"type":"","data":{}}`), ErrMissingType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			env, err := Decode(tt.input)

			// Assert
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Decode(%s) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if env.Type != "" || env.Data != nil {
				t.Errorf("Decode(%s) envelope = %+v, want zero value", tt.input, env)
			}
		})
	}
}
