package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestCORS(t *testing.T) {
	t.Run("an allowed origin gets echoed CORS headers", func(t *testing.T) {
		handler := CORS([]string{"http://localhost:3000"})(okHandler())

		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Errorf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
		}
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want %q", got, "Origin")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("body = %q, want %q (inner handler must have run)", rec.Body.String(), "ok")
		}
	})

	t.Run("a disallowed origin gets no CORS headers but is not blocked", func(t *testing.T) {
		handler := CORS([]string{"http://localhost:3000"})(okHandler())

		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		req.Header.Set("Origin", "http://evil.test")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want none", got)
		}
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want %q even for a disallowed origin", got, "Origin")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d (server-side block is not this middleware's job)", rec.Code, http.StatusOK)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("body = %q, want %q (inner handler must have run)", rec.Body.String(), "ok")
		}
	})

	t.Run("no Origin header at all gets no CORS headers", func(t *testing.T) {
		handler := CORS([]string{"http://localhost:3000"})(okHandler())

		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want none", got)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}
