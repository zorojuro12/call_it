package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteData(t *testing.T) {
	t.Run("with a payload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteData(rec, 201, map[string]any{"id": "abc"})

		if rec.Code != 201 {
			t.Errorf("status = %d, want 201", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response body does not decode as JSON: %v", err)
		}
		want := map[string]any{"data": map[string]any{"id": "abc"}}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("body = %s, want %s", gotJSON, wantJSON)
		}
	})

	t.Run("with nil", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteData(rec, 200, nil)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response body does not decode as JSON: %v", err)
		}
		if v, ok := got["data"]; !ok || v != nil {
			t.Errorf("body's data field = %v (present=%v), want null", v, ok)
		}
	})
}
