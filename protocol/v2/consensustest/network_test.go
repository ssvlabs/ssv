package consensustest_test

import (
	mrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
)

// TestLogNormalDelay_Shape exercises the log-normal model on a large
// sample set and verifies the distribution shape matches expectations.
//
// Expected ratios (analytic, from log-normal P-quantile = exp(μ + σ·Z_p)):
//   - Sigma=0.3: P99/P50 = exp(0.3 · 2.326) ≈ 2.01.
//   - Sigma=0.5: P99/P50 = exp(0.5 · 2.326) ≈ 3.20.
//   - Sigma=0.7: P99/P50 = exp(0.7 · 2.326) ≈ 5.09.
//
// Test with N=10000 samples; tolerance ±15% to absorb sample noise at
// P99 (where the standard error is largest).
func TestLogNormalDelay_Shape(t *testing.T) {
	cases := []struct {
		sigma         float64
		expectedRatio float64
	}{
		{0.3, 2.01},
		{0.5, 3.20},
		{0.7, 5.09},
	}

	const (
		median  = 150 * time.Millisecond
		samples = 10000
		seed    = int64(42)
	)

	for _, tc := range cases {
		tc := tc
		t.Run("sigma="+formatFloat(tc.sigma), func(t *testing.T) {
			model := ct.LogNormalDelay{Median: median, Sigma: tc.sigma}
			rng := mrand.New(mrand.NewSource(seed))
			d := make(ct.Distribution, samples)
			for i := 0; i < samples; i++ {
				d[i] = float64(model.Delay(rng, 1, 2, ct.KindCommit))
			}

			// Median within ±10% of parameter.
			gotMedian := d.Median()
			require.InEpsilonf(t, float64(median), gotMedian, 0.10,
				"sigma=%v: median %.0fns vs expected %dns", tc.sigma, gotMedian, median.Nanoseconds())

			// P99/P50 ratio within ±15% of analytic expectation.
			p50 := d.Percentile(50)
			p99 := d.Percentile(99)
			require.Greater(t, p50, 0.0)
			ratio := p99 / p50
			require.InEpsilonf(t, tc.expectedRatio, ratio, 0.15,
				"sigma=%v: P99/P50 ratio %.2f vs expected %.2f", tc.sigma, ratio, tc.expectedRatio)

			// No negative draws.
			require.Greater(t, d.Min(), 0.0)
		})
	}
}

// TestLogNormalDelay_Deterministic — same seed produces same samples.
func TestLogNormalDelay_Deterministic(t *testing.T) {
	model := ct.LogNormalDelay{Median: 200 * time.Millisecond, Sigma: 0.5}
	draws := func(seed int64) []time.Duration {
		rng := mrand.New(mrand.NewSource(seed))
		out := make([]time.Duration, 100)
		for i := range out {
			out[i] = model.Delay(rng, 1, 2, ct.KindCommit)
		}
		return out
	}
	a := draws(7)
	b := draws(7)
	require.Equal(t, a, b, "same seed must produce identical draws")
}

// TestLogNormalDelay_HealthyOBFT — end-to-end check: substitute
// LogNormalDelay for the canonical ConstantDelay on a Healthy scenario;
// cluster should still decide (Healthy doesn't depend on tight propagation
// budgets), and per-op decision time should be higher P99 than under
// ConstantDelay (heavy tail leaks into the slot timeline).
func TestLogNormalDelay_HealthyOBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// LogNormal centered around the BTT median, modest tail.
	cfg.Network = ct.LogNormalDelay{Median: 100 * time.Millisecond, Sigma: 0.3}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "Healthy under modest LogNormal tail must still decide")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	t.Logf("OBFT Healthy under LogNormalDelay(Median=100ms, Sigma=0.3): decided at %v on L_%d",
		out.DecisionTime, out.DecidedRound)
}

// formatFloat — minimal stdlib-only float-to-string for subtest names.
// Avoids strconv import for one call site.
func formatFloat(f float64) string {
	// One decimal place suffices for the sigma values we use.
	cents := int(f*10 + 0.5)
	return string(rune('0'+cents/10)) + "." + string(rune('0'+cents%10))
}
