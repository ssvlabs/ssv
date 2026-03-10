package stats

import (
	"math"
	"sync/atomic"
)

// Histogram is a simple fixed-bucket histogram for float64 values.
//
// It is intended for low-frequency, in-process summary reporting (e.g. hourly logs),
// not high-throughput metrics export.
type Histogram struct {
	// bounds are the inclusive upper bounds for each bucket, in ascending order.
	bounds []float64
	// counts has len(bounds)+1 buckets. The last bucket is the overflow bucket (> last bound).
	counts []uint64

	sumBits uint64 // atomic float64 sum (math.Float64bits)
	total   uint64 // atomic count
}

func NewHistogram(bounds []float64) *Histogram {
	c := make([]uint64, len(bounds)+1)
	return &Histogram{
		bounds: append([]float64(nil), bounds...),
		counts: c,
	}
}

func (h *Histogram) Record(v float64) {
	if h == nil {
		return
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	if v < 0 {
		v = 0
	}

	// Update sum and total atomically.
	for {
		oldBits := atomic.LoadUint64(&h.sumBits)
		old := math.Float64frombits(oldBits)
		newBits := math.Float64bits(old + v)
		if atomic.CompareAndSwapUint64(&h.sumBits, oldBits, newBits) {
			break
		}
	}
	atomic.AddUint64(&h.total, 1)

	// Bucket counts are not atomic; caller is expected to protect with a mutex for correctness.
	// This is fine for our low-frequency observation paths.
	idx := len(h.bounds) // overflow
	for i, b := range h.bounds {
		if v <= b {
			idx = i
			break
		}
	}
	h.counts[idx]++
}

func (h *Histogram) Count() uint64 {
	if h == nil {
		return 0
	}
	return atomic.LoadUint64(&h.total)
}

func (h *Histogram) Sum() float64 {
	if h == nil {
		return 0
	}
	return math.Float64frombits(atomic.LoadUint64(&h.sumBits))
}

// QuantileUpperBound returns the bucket upper bound containing the given quantile.
// This intentionally returns an approximation based on bucket boundaries.
func (h *Histogram) QuantileUpperBound(q float64) (float64, bool) {
	if h == nil {
		return 0, false
	}
	if q <= 0 || q > 1 {
		return 0, false
	}

	total := atomic.LoadUint64(&h.total)
	if total == 0 {
		return 0, false
	}

	target := uint64(math.Ceil(float64(total) * q))
	if target == 0 {
		target = 1
	}

	var cum uint64
	for i := range h.counts {
		cum += h.counts[i]
		if cum >= target {
			if i >= len(h.bounds) {
				return h.bounds[len(h.bounds)-1], true
			}
			return h.bounds[i], true
		}
	}

	return h.bounds[len(h.bounds)-1], true
}

func (h *Histogram) Reset() {
	if h == nil {
		return
	}
	for i := range h.counts {
		h.counts[i] = 0
	}
	atomic.StoreUint64(&h.sumBits, 0)
	atomic.StoreUint64(&h.total, 0)
}
