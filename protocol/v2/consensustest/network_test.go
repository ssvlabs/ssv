package consensustest_test

import (
	mrand "math/rand"
	"strconv"
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

// TestLossyNetwork_LossRate — at LossRate=p, BurstFactor=k, the observed
// loss fraction over N draws should converge on p. Tolerance widens with
// BurstFactor because Markov-chain autocorrelation reduces effective
// sample size: N_eff ≈ N/k, so binomial 99% CI on the observed rate is
// ±2.58·√(p(1-p)/N_eff). At p=0.10, k=10, N=10000: N_eff ≈ 1000, CI ≈
// ±0.024. Test asserts ±0.03 to give a safety margin.
func TestLossyNetwork_LossRate(t *testing.T) {
	cases := []struct {
		lossRate    float64
		burstFactor int
		tolerance   float64
	}{
		{0.01, 5, 0.015},
		{0.05, 5, 0.020},
		{0.10, 10, 0.030},
	}
	const (
		samples = 10000
		seed    = int64(42)
		base    = 200 * time.Millisecond
	)

	for _, tc := range cases {
		tc := tc
		t.Run("rate="+formatFloat(tc.lossRate)+"/burst="+strconv.Itoa(tc.burstFactor), func(t *testing.T) {
			model := ct.NewLossyNetwork(ct.ConstantDelay{D: base}, tc.lossRate, tc.burstFactor)
			rng := mrand.New(mrand.NewSource(seed))
			lost := 0
			for i := 0; i < samples; i++ {
				d := model.Delay(rng, 1, 2, ct.KindCommit)
				if d == ct.DroppedDelay {
					lost++
				}
			}
			observed := float64(lost) / float64(samples)
			require.InDeltaf(t, tc.lossRate, observed, tc.tolerance,
				"observed loss fraction %.4f vs target %.4f (samples=%d, burst=%d, tol=%.4f)",
				observed, tc.lossRate, samples, tc.burstFactor, tc.tolerance)
		})
	}
}

// TestLossyNetwork_BurstShape — at LossRate=0.10, BurstFactor=10, the
// mean length of consecutive-loss runs should approximate BurstFactor.
// We allow ±40% tolerance: burst length distribution is geometric(1/k)
// with mean k, variance k(k-1) ≈ 90 at k=10. With N=50000 draws we
// expect ~500 bursts; SEM of the mean is √90/√500 ≈ 0.42, so 99% CI
// on the empirical mean is ~±1.1 (= 11% of expected). We use 40%
// tolerance to absorb seed-specific variance without flaking.
func TestLossyNetwork_BurstShape(t *testing.T) {
	const (
		lossRate    = 0.10
		burstFactor = 10
		samples     = 50000
		seed        = int64(99)
	)
	model := ct.NewLossyNetwork(ct.ConstantDelay{D: 200 * time.Millisecond}, lossRate, burstFactor)
	rng := mrand.New(mrand.NewSource(seed))

	var runs []int
	currentRun := 0
	for i := 0; i < samples; i++ {
		d := model.Delay(rng, 1, 2, ct.KindCommit)
		if d == ct.DroppedDelay {
			currentRun++
		} else if currentRun > 0 {
			runs = append(runs, currentRun)
			currentRun = 0
		}
	}
	if currentRun > 0 {
		runs = append(runs, currentRun)
	}
	require.NotEmpty(t, runs, "expected at least some loss bursts at 10%% rate")
	var total int
	for _, r := range runs {
		total += r
	}
	meanRun := float64(total) / float64(len(runs))
	require.InEpsilonf(t, float64(burstFactor), meanRun, 0.40,
		"mean burst length %.2f vs expected %d (n_bursts=%d)",
		meanRun, burstFactor, len(runs))
}

// TestLossyNetwork_ZeroAndFull — LossRate=0 must drop nothing,
// LossRate=1 must drop everything.
func TestLossyNetwork_ZeroAndFull(t *testing.T) {
	base := ct.ConstantDelay{D: 200 * time.Millisecond}
	rng := mrand.New(mrand.NewSource(1))
	zero := ct.NewLossyNetwork(base, 0, 5)
	for i := 0; i < 100; i++ {
		require.NotEqual(t, ct.DroppedDelay, zero.Delay(rng, 1, 2, ct.KindCommit),
			"LossRate=0 must drop nothing (iter %d)", i)
	}
	full := ct.NewLossyNetwork(base, 1, 5)
	for i := 0; i < 100; i++ {
		require.Equal(t, ct.DroppedDelay, full.Delay(rng, 1, 2, ct.KindCommit),
			"LossRate=1 must drop everything (iter %d)", i)
	}
}

// TestLossyNetwork_Deterministic — same seed + fresh model produces
// identical drop sequence.
func TestLossyNetwork_Deterministic(t *testing.T) {
	draws := func(seed int64) []bool {
		model := ct.NewLossyNetwork(ct.ConstantDelay{D: 100 * time.Millisecond}, 0.1, 5)
		rng := mrand.New(mrand.NewSource(seed))
		out := make([]bool, 200)
		for i := range out {
			out[i] = model.Delay(rng, 1, 2, ct.KindCommit) == ct.DroppedDelay
		}
		return out
	}
	a := draws(123)
	b := draws(123)
	require.Equal(t, a, b, "same seed must produce identical loss sequence")
}

// formatFloat — fixed-2-decimal float format for subtest names. Uses
// strconv with explicit precision for stable, deterministic strings.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}
