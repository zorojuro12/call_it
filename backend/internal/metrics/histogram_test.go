package metrics

import (
	"testing"
	"time"
)

func TestHistogramObserveAndQuantile(t *testing.T) {
	t.Run("no observations", func(t *testing.T) {
		h := NewHistogram()
		got, ok := h.Quantile(0.99)
		if ok || got != 0 {
			t.Fatalf("Quantile() = (%v, %v), want (0, false)", got, ok)
		}
	})

	t.Run("single sample in the 5ms bucket", func(t *testing.T) {
		h := NewHistogram()
		h.Observe(3 * time.Millisecond)
		got, ok := h.Quantile(0.99)
		if !ok || got != 5*time.Millisecond {
			t.Fatalf("Quantile(0.99) = (%v, %v), want (5ms, true)", got, ok)
		}
	})

	t.Run("100 samples at 1ms", func(t *testing.T) {
		h := NewHistogram()
		for i := 0; i < 100; i++ {
			h.Observe(1 * time.Millisecond)
		}
		got, ok := h.Quantile(0.99)
		if !ok || got != 1*time.Millisecond {
			t.Fatalf("Quantile(0.99) = (%v, %v), want (1ms, true)", got, ok)
		}
		if h.Count() != 100 {
			t.Fatalf("Count() = %d, want 100", h.Count())
		}
		if h.Sum() != 100*time.Millisecond {
			t.Fatalf("Sum() = %v, want 100ms", h.Sum())
		}
	})

	t.Run("99 samples at 1ms plus one outlier at 40ms", func(t *testing.T) {
		h := NewHistogram()
		for i := 0; i < 99; i++ {
			h.Observe(1 * time.Millisecond)
		}
		h.Observe(40 * time.Millisecond)

		got, ok := h.Quantile(0.99)
		if !ok || got != 1*time.Millisecond {
			t.Fatalf("Quantile(0.99) = (%v, %v), want (1ms, true)", got, ok)
		}
		got, ok = h.Quantile(1.0)
		if !ok || got != 50*time.Millisecond {
			t.Fatalf("Quantile(1.0) = (%v, %v), want (50ms, true)", got, ok)
		}
	})

	t.Run("sample exactly on a boundary belongs to that bucket", func(t *testing.T) {
		h := NewHistogram()
		h.Observe(15 * time.Millisecond)
		got, ok := h.Quantile(0.5)
		if !ok || got != 15*time.Millisecond {
			t.Fatalf("Quantile(0.5) = (%v, %v), want (15ms, true)", got, ok)
		}
	})

	t.Run("zero sample falls in the smallest bucket", func(t *testing.T) {
		h := NewHistogram()
		h.Observe(0)
		got, ok := h.Quantile(0.5)
		if !ok || got != 500*time.Microsecond {
			t.Fatalf("Quantile(0.5) = (%v, %v), want (500us, true)", got, ok)
		}
	})

	t.Run("negative sample is dropped, not folded into the first bucket", func(t *testing.T) {
		h := NewHistogram()
		h.Observe(-1 * time.Millisecond)
		if h.Count() != 0 {
			t.Fatalf("Count() = %d, want 0", h.Count())
		}
		got, ok := h.Quantile(0.5)
		if ok || got != 0 {
			t.Fatalf("Quantile(0.5) = (%v, %v), want (0, false)", got, ok)
		}
	})
}
