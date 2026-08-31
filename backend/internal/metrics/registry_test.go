package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryGetOrCreate(t *testing.T) {
	r := NewRegistry()

	h1 := r.Histogram("wager_place_ok")
	h2 := r.Histogram("wager_place_ok")
	if h1 != h2 {
		t.Fatalf("Histogram() returned different pointers for the same name")
	}

	c1 := r.Counter("ws_send_dropped")
	c2 := r.Counter("ws_send_dropped")
	if c1 != c2 {
		t.Fatalf("Counter() returned different pointers for the same name")
	}
}

func TestRegistryHistogramAndCounterDoNotCollide(t *testing.T) {
	r := NewRegistry()

	h := r.Histogram("wager_place_ok")
	c := r.Counter("wager_place_ok")

	h.Observe(1 * time.Millisecond)
	c.Inc()

	if h.Count() != 1 {
		t.Fatalf("Histogram Count() = %d, want 1 (must not be affected by Counter.Inc)", h.Count())
	}
	if c.Value() != 1 {
		t.Fatalf("Counter Value() = %d, want 1 (must not be affected by Histogram.Observe)", c.Value())
	}
}

func TestRegistryRender(t *testing.T) {
	t.Run("empty registry renders empty string", func(t *testing.T) {
		r := NewRegistry()
		if got := r.Render(); got != "" {
			t.Fatalf("Render() = %q, want empty string", got)
		}
	})

	t.Run("exact rendered output, sorted by name", func(t *testing.T) {
		r := NewRegistry()
		r.Histogram("wager_place_ok").Observe(3 * time.Millisecond)
		r.Counter("ws_send_dropped").Inc()

		want := "callit_wager_place_ok_count 1\n" +
			"callit_wager_place_ok_p50_ms 5\n" +
			"callit_wager_place_ok_p99_ms 5\n" +
			"callit_wager_place_ok_sum_ms 3\n" +
			"callit_ws_send_dropped 1\n"

		if got := r.Render(); got != want {
			t.Fatalf("Render() = %q, want %q", got, want)
		}
	})
}

func TestMetricNamesAreStable(t *testing.T) {
	r := NewRegistry()
	r.Histogram(NameWagerPlaceOK)
	r.Histogram(NameWagerPlaceErr)
	r.Histogram(NameWSSync)
	r.Counter(NameWSSendDropped)

	rendered := r.Render()
	prefixes := []string{
		"callit_wager_place_ok_",
		"callit_wager_place_err_",
		"callit_ws_sync_",
		"callit_ws_send_dropped",
	}
	for _, p := range prefixes {
		if !strings.Contains(rendered, p) {
			t.Errorf("Render() = %q, want it to contain a line starting %q", rendered, p)
		}
	}
}

func TestHandler(t *testing.T) {
	r := NewRegistry()
	r.Histogram("wager_place_ok").Observe(3 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Handler(r).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want %q", ct, "text/plain; charset=utf-8")
	}
	if rec.Body.String() != r.Render() {
		t.Fatalf("body = %q, want %q", rec.Body.String(), r.Render())
	}
}
