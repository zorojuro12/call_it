package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing count. Safe for concurrent use.
type Counter struct {
	value atomic.Uint64
}

// Inc increments the counter by one.
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Value returns the counter's current value.
func (c *Counter) Value() uint64 {
	return c.value.Load()
}

// Registry is a named collection of histograms and counters. Registration
// is rare and guarded by a mutex; Observe/Inc on an already-registered
// metric never touches the mutex.
type Registry struct {
	mu         sync.Mutex
	histograms map[string]*Histogram
	counters   map[string]*Counter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		histograms: make(map[string]*Histogram),
		counters:   make(map[string]*Counter),
	}
}

// Histogram returns the named histogram, creating it on first use. The
// same name always returns the same pointer.
func (r *Registry) Histogram(name string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.histograms[name]
	if !ok {
		h = NewHistogram()
		r.histograms[name] = h
	}
	return h
}

// Counter returns the named counter, creating it on first use. The same
// name always returns the same pointer.
func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.counters[name]
	if !ok {
		c = &Counter{}
		r.counters[name] = c
	}
	return c
}

// Render returns every registered metric as line-oriented text, sorted by
// emitted metric name ascending. A histogram named "x" renders four
// lines: callit_x_count, callit_x_p50_ms, callit_x_p99_ms,
// callit_x_sum_ms. A counter named "x" renders one line: callit_x.
// Milliseconds render as integers; a quantile in the overflow bucket
// renders as -1.
func (r *Registry) Render() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lines []string
	for name, h := range r.histograms {
		p50, _ := h.Quantile(0.5)
		p99, _ := h.Quantile(0.99)
		lines = append(lines,
			fmt.Sprintf("callit_%s_count %d", name, h.Count()),
			fmt.Sprintf("callit_%s_p50_ms %d", name, quantileMillis(p50)),
			fmt.Sprintf("callit_%s_p99_ms %d", name, quantileMillis(p99)),
			fmt.Sprintf("callit_%s_sum_ms %d", name, h.Sum().Milliseconds()),
		)
	}
	for name, c := range r.counters {
		lines = append(lines, fmt.Sprintf("callit_%s %d", name, c.Value()))
	}

	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func quantileMillis(d time.Duration) int64 {
	if d == OverTopBucket {
		return -1
	}
	return d.Milliseconds()
}

// Handler returns an http.Handler that serves the registry's Render()
// output as plain text.
func Handler(r *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Render()))
	})
}
