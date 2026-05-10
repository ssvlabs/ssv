package consensustest_test

import (
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// TestDistribution_Empty — empty-distribution methods return zero per
// documented contract; no panic.
func TestDistribution_Empty(t *testing.T) {
	d := ct.Distribution{}
	require.Equal(t, 0, d.Len())
	require.Equal(t, 0.0, d.Mean())
	require.Equal(t, 0.0, d.Median())
	require.Equal(t, 0.0, d.Min())
	require.Equal(t, 0.0, d.Max())
	require.Equal(t, 0.0, d.Stddev())
	require.Equal(t, 0.0, d.Percentile(50))
	require.Equal(t, 0.0, d.Percentile(0))
	require.Equal(t, 0.0, d.Percentile(100))
}

// TestDistribution_SingleSample — methods on a one-sample distribution
// return the sample for percentile / min / max / median / mean.
func TestDistribution_SingleSample(t *testing.T) {
	d := ct.Distribution{42.0}
	require.Equal(t, 1, d.Len())
	require.Equal(t, 42.0, d.Mean())
	require.Equal(t, 42.0, d.Median())
	require.Equal(t, 42.0, d.Min())
	require.Equal(t, 42.0, d.Max())
	require.Equal(t, 0.0, d.Stddev()) // no variance with one sample
	require.Equal(t, 42.0, d.Percentile(50))
	require.Equal(t, 42.0, d.Percentile(0))
	require.Equal(t, 42.0, d.Percentile(100))
}

// TestDistribution_KnownValues — exercises the percentile interpolation
// on a small hand-checked input.
func TestDistribution_KnownValues(t *testing.T) {
	// 1, 2, 3, 4, 5 — Min=1, Max=5, Median=3, Mean=3.
	d := ct.Distribution{1, 2, 3, 4, 5}
	require.Equal(t, 5, d.Len())
	require.Equal(t, 3.0, d.Mean())
	require.Equal(t, 3.0, d.Median())
	require.Equal(t, 1.0, d.Min())
	require.Equal(t, 5.0, d.Max())
	require.Equal(t, 1.0, d.Percentile(0))
	require.Equal(t, 5.0, d.Percentile(100))
	// P25 interpolates between sorted[1]=2 and sorted[2]=3 at position
	// (25/100)*(5-1) = 1.0 → exactly sorted[1] = 2.0.
	require.Equal(t, 2.0, d.Percentile(25))
	// P75 interpolates at position (75/100)*4 = 3.0 → exactly sorted[3] = 4.0.
	require.Equal(t, 4.0, d.Percentile(75))
	// Stddev on [1..5] with mean=3 → sqrt((4+1+0+1+4)/5) = sqrt(2) ≈ 1.414.
	require.InDelta(t, math.Sqrt(2), d.Stddev(), 1e-9)
}

// TestDistribution_OutOfOrder — input order shouldn't affect summary
// methods (internal sorting is per-call, not stateful).
func TestDistribution_OutOfOrder(t *testing.T) {
	a := ct.Distribution{5, 3, 1, 4, 2}
	b := ct.Distribution{1, 2, 3, 4, 5}
	require.Equal(t, b.Mean(), a.Mean())
	require.Equal(t, b.Median(), a.Median())
	require.Equal(t, b.Min(), a.Min())
	require.Equal(t, b.Max(), a.Max())
	require.Equal(t, b.Percentile(25), a.Percentile(25))
	require.Equal(t, b.Percentile(75), a.Percentile(75))
}

// TestDistribution_MonotoneIncreasingPercentile — for any sample set,
// Percentile is non-decreasing in p (basic invariant for charting).
func TestDistribution_MonotoneIncreasingPercentile(t *testing.T) {
	// Random-ish sample (deterministic in source).
	d := ct.Distribution{7, 2, 9, 1, 8, 3, 6, 4, 5}
	var prev float64
	for p := 0.0; p <= 100; p += 5 {
		v := d.Percentile(p)
		require.GreaterOrEqual(t, v, prev,
			"Percentile must be non-decreasing in p (p=%v gave %v, prev %v)", p, v, prev)
		prev = v
	}
}

// TestDistribution_LogNormalSampling — sanity-check that the math works on
// log-normal-shaped samples: median is finite, mean ≥ median (right-skewed),
// percentile ordering holds.
func TestDistribution_LogNormalSampling(t *testing.T) {
	// Approximate log-normal: take samples x_i = exp(z_i) for z_i ∈ [-1, 1]
	// linearly spaced. Resulting distribution is right-skewed.
	n := 100
	d := make(ct.Distribution, n)
	for i := 0; i < n; i++ {
		z := -1.0 + 2.0*float64(i)/float64(n-1)
		d[i] = math.Exp(z)
	}
	median := d.Median()
	mean := d.Mean()
	require.Greater(t, median, 0.0)
	// Right-skewed: mean > median for log-normal.
	require.Greater(t, mean, median, "log-normal must have mean > median")
	// P99 > P90 > P50.
	require.Greater(t, d.Percentile(99), d.Percentile(90))
	require.Greater(t, d.Percentile(90), d.Percentile(50))
	// Stddev positive.
	require.Greater(t, d.Stddev(), 0.0)
}

// TestDistribution_DoesNotMutateInput — Percentile / Min / Max / etc. must
// not reorder the caller's slice (internal sorting works on a copy).
func TestDistribution_DoesNotMutateInput(t *testing.T) {
	orig := []float64{5, 3, 1, 4, 2}
	d := ct.Distribution(append([]float64(nil), orig...))
	_ = d.Percentile(50)
	_ = d.Min()
	_ = d.Max()
	_ = d.Median()
	// d should still match orig (not sorted).
	require.Equal(t, orig, []float64(d), "Distribution methods must not mutate the underlying slice")
	// Sanity: control sort of the same content gives different order.
	sorted := append([]float64(nil), orig...)
	sort.Float64s(sorted)
	require.NotEqual(t, sorted, orig, "test setup error — orig must not already be sorted")
}
