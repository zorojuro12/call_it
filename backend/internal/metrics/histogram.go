// Package metrics is a pure, dependency-free latency histogram and named
// registry. No I/O beyond Handler, which renders the registry as text.
package metrics

import (
	"math"
	"sort"
	"time"
)

// Bounds are the fixed histogram bucket upper bounds, ascending. They sit
// on both spec SLA values (15ms, 30ms) so neither target needs a bucket
// straddled.
var Bounds = []time.Duration{
	500 * time.Microsecond,
	1 * time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	15 * time.Millisecond,
	20 * time.Millisecond,
	30 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
}

// OverTopBucket is returned by Quantile when the requested quantile falls
// in the overflow bucket — a sample larger than the largest bound.
var OverTopBucket = time.Duration(-1)

// Histogram is a fixed-bucket latency histogram with no interpolation:
// Quantile(q) returns the upper bound of the lowest bucket whose
// cumulative count reaches ceil(q * N). A Quantile/Count/Sum read taken
// concurrently with Observe may see bucket counts that do not yet sum to
// Count() — callers needing an exact snapshot must quiesce writers first.
type Histogram struct {
	buckets  []uint64
	count    uint64
	sumNanos int64
}

// NewHistogram returns an empty histogram. The bucket array has
// len(Bounds)+1 entries; the last is the overflow bucket for samples
// larger than the largest bound.
func NewHistogram() *Histogram {
	return &Histogram{buckets: make([]uint64, len(Bounds)+1)}
}

// Observe records one sample. A negative duration means the caller's
// clock went backwards; it is dropped rather than folded into the first
// bucket, where it would silently improve the reported quantiles. A
// sample larger than the largest bound is counted in the overflow
// bucket, never dropped — a p99 that silently discards its slowest
// samples would report a target as met when it was missed.
func (h *Histogram) Observe(d time.Duration) {
	if d < 0 {
		return
	}
	idx := sort.Search(len(Bounds), func(i int) bool { return Bounds[i] >= d })
	h.buckets[idx]++
	h.count++
	h.sumNanos += int64(d)
}

// Quantile returns the upper bound of the lowest bucket whose cumulative
// count reaches ceil(q * N), or OverTopBucket when that count is only
// reached in the overflow bucket. ok is false when no samples have been
// observed.
func (h *Histogram) Quantile(q float64) (time.Duration, bool) {
	if h.count == 0 {
		return 0, false
	}
	target := uint64(math.Ceil(q * float64(h.count)))
	var cum uint64
	for i, c := range h.buckets {
		cum += c
		if cum >= target {
			if i == len(Bounds) {
				return OverTopBucket, true
			}
			return Bounds[i], true
		}
	}
	return 0, false
}

// Count returns the total number of samples observed.
func (h *Histogram) Count() uint64 {
	return h.count
}

// Sum returns the total duration of all samples observed.
func (h *Histogram) Sum() time.Duration {
	return time.Duration(h.sumNanos)
}
