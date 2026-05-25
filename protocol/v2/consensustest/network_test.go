package consensustest_test

import (
	mrand "math/rand"
	"sort"
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

// TestCorrelatedLinkDelay_PerPairFraction — at BadLinkProb=p across
// many pairs and many calls, the per-pair time-fraction-in-bad-state
// should average to p. Test sums bad-state observations across all
// pairs and verifies the fraction matches.
//
// Note on pair semantics: CorrelatedLinkDelay is per-undirected-pair —
// (A→B) and (B→A) share one Markov chain — so n=4 has 6 distinct
// pair-chains observed via 12 directed visits per outer iteration.
// The fraction-in-bad steady-state doesn't depend on chain count, so
// the long-run fraction check is unaffected.
func TestCorrelatedLinkDelay_PerPairFraction(t *testing.T) {
	const (
		badProb       = 0.20
		badMul        = 3.0
		burstMessages = 10
		callsPerPair  = 1000
		seed          = int64(7)
		base          = 200 * time.Millisecond
	)
	// 12 directed visits per outer iter (4 ops × 3 receivers excluding self =
	// 12; 6 undirected pair-chains). Folded into the require.InDeltaf message
	// below for diagnostic readability; the loop iterates the cross-product
	// directly, no separate variable needed.

	model := ct.NewCorrelatedLinkDelay(ct.ConstantDelay{D: base}, badProb, badMul, burstMessages)
	rng := mrand.New(mrand.NewSource(seed))

	badCount := 0
	totalCount := 0
	for call := 0; call < callsPerPair; call++ {
		for from := ct.OperatorID(1); from <= 4; from++ {
			for to := ct.OperatorID(1); to <= 4; to++ {
				if from == to {
					continue
				}
				d := model.Delay(rng, from, to, ct.KindCommit)
				if d > base { // bad-state inflates delay above base
					badCount++
				}
				totalCount++
			}
		}
	}
	observedFrac := float64(badCount) / float64(totalCount)
	// Tolerance: per-pair Markov autocorrelation reduces effective N by
	// ~BurstMessages per pair; pooling across 12 pairs and 1000 calls
	// = effective ~1200 samples. Binomial 99% CI at p=0.2 is ±0.030.
	require.InDeltaf(t, badProb, observedFrac, 0.04,
		"observed bad-state fraction %.4f vs target %.4f (calls=%d pairs=%d)",
		observedFrac, badProb, callsPerPair, 12)
}

// TestCorrelatedLinkDelay_MultiplierApplied — when a link is bad, the
// observed delay matches base × BadLinkMultiplier. When good, matches
// base. (Sanity test on the per-call branch logic.)
func TestCorrelatedLinkDelay_MultiplierApplied(t *testing.T) {
	const base = 100 * time.Millisecond
	model := ct.NewCorrelatedLinkDelay(ct.ConstantDelay{D: base}, 0.5, 4.0, 20)
	rng := mrand.New(mrand.NewSource(11))

	good, bad := 0, 0
	totalGood, totalBad := time.Duration(0), time.Duration(0)
	for i := 0; i < 1000; i++ {
		d := model.Delay(rng, 1, 2, ct.KindCommit)
		switch d {
		case base:
			good++
			totalGood += d
		case 4 * base: // multiplier=4 → bad delay = 400ms
			bad++
			totalBad += d
		default:
			t.Fatalf("unexpected delay %v (base=%v, expected base or 4·base)", d, base)
		}
	}
	require.Greater(t, good, 0, "expected at least some good-link draws")
	require.Greater(t, bad, 0, "expected at least some bad-link draws (BadLinkProb=0.5)")
}

// TestCorrelatedLinkDelay_Deterministic — same seed + fresh model
// produces identical delay sequence.
func TestCorrelatedLinkDelay_Deterministic(t *testing.T) {
	draws := func(seed int64) []time.Duration {
		model := ct.NewCorrelatedLinkDelay(ct.ConstantDelay{D: 100 * time.Millisecond}, 0.3, 3.0, 10)
		rng := mrand.New(mrand.NewSource(seed))
		out := make([]time.Duration, 500)
		for i := range out {
			out[i] = model.Delay(rng, ct.OperatorID(i%4+1), ct.OperatorID((i+1)%4+1), ct.KindCommit)
		}
		return out
	}
	a := draws(55)
	b := draws(55)
	require.Equal(t, a, b, "same seed must produce identical delay sequence")
}

// TestMarkovianSlowness_Additive verifies that the slow state adds ExtraDelay
// on top of Inner.Delay rather than replacing it. The first call for a new
// link is always in the slow state (spec: slot starts mid-bad-period), so
// the returned delay must equal Inner.Delay + ExtraDelay.
func TestMarkovianSlowness_Additive(t *testing.T) {
	const (
		base  = 100 * time.Millisecond
		extra = 200 * time.Millisecond
	)
	model := ct.NewMarkovianSlowness(ct.ConstantDelay{D: base}, []ct.OperatorID{2}, extra, 0.8)
	rng := mrand.New(mrand.NewSource(42))

	d := model.Delay(rng, 1, 2, ct.KindCommit)
	require.Equal(t, base+extra, d,
		"slow state must return Inner.Delay + ExtraDelay, not ExtraDelay alone")
}

// TestMarkovianSlowness_PairIndependence verifies that two links sharing a
// slow op (e.g. op2) each start in the slow state independently. Under the
// old per-op model the second link would inherit the already-inited op2
// chain and potentially be in fast state; with per-pair state both links
// must be slow on their first call.
func TestMarkovianSlowness_PairIndependence(t *testing.T) {
	const (
		base  = 100 * time.Millisecond
		extra = 200 * time.Millisecond
	)
	// PersistP=1: chain never flips after init, so slow state is permanent
	// once entered. This rules out coincidental slow results.
	model := ct.NewMarkovianSlowness(ct.ConstantDelay{D: base}, []ct.OperatorID{2}, extra, 1.0)
	rng := mrand.New(mrand.NewSource(7))

	// Both pairs involve slow op2 but are distinct links.
	d12 := model.Delay(rng, 1, 2, ct.KindCommit)
	d32 := model.Delay(rng, 3, 2, ct.KindCommit)

	require.Equal(t, base+extra, d12, "link (1,2): first call must be slow")
	require.Equal(t, base+extra, d32, "link (3,2): must start slow independently of (1,2)")
}

// TestLossyNetwork_PairIndependence verifies that different links have
// independent loss chains. With a high BurstFactor the per-link Markov
// chains exhibit sustained bursts; if the chains were shared (old global
// model), two disjoint links would both drop simultaneously whenever the
// single chain is in bad state, producing a co-drop rate equal to LossRate.
// With independent per-link chains the co-drop rate converges on LossRate².
func TestLossyNetwork_PairIndependence(t *testing.T) {
	const (
		lossRate    = 0.5
		burstFactor = 50 // high burst amplifies correlation when chains are shared
		samples     = 10000
		seed        = int64(42)
	)
	model := ct.NewLossyNetwork(ct.ConstantDelay{D: 200 * time.Millisecond}, lossRate, burstFactor)
	rng := mrand.New(mrand.NewSource(seed))

	bothDrop := 0
	for i := 0; i < samples; i++ {
		d12 := model.Delay(rng, 1, 2, ct.KindCommit)
		d34 := model.Delay(rng, 3, 4, ct.KindCommit)
		if d12 == ct.DroppedDelay && d34 == ct.DroppedDelay {
			bothDrop++
		}
	}
	observed := float64(bothDrop) / float64(samples)
	// Independent chains: P(both drop) ≈ LossRate² = 0.25.
	// Fully correlated (old global chain): P(both drop) ≈ LossRate = 0.50.
	// Tolerance ±0.06 is ~2× the std error at N_eff ≈ samples/burstFactor = 200.
	require.InDeltaf(t, lossRate*lossRate, observed, 0.06,
		"co-drop rate %.4f; independent chains expect ~%.2f, shared chain would give ~%.2f",
		observed, lossRate*lossRate, lossRate)
}

// formatFloat — fixed-2-decimal float format for subtest names. Uses
// strconv with explicit precision for stable, deterministic strings.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// TestLogNormalMixture_ComponentSelectionWeights — the inverse-CDF
// component picker should fire each component at its declared weight.
// Big sample, tight tolerance.
func TestLogNormalMixture_ComponentSelectionWeights(t *testing.T) {
	mix := ct.NewLogNormalMixtureDelay([]ct.LogNormalComponent{
		{Weight: 0.25, Median: 100 * time.Microsecond, Sigma: 0.01},
		{Weight: 0.55, Median: 10000 * time.Microsecond, Sigma: 0.01},
		{Weight: 0.20, Median: 1000000 * time.Microsecond, Sigma: 0.01},
	})
	// Components have order-of-magnitude separated medians and tiny σ so
	// each sample's order of magnitude identifies its source component.
	const N = 20000
	rng := mrand.New(mrand.NewSource(42))
	counts := [3]int{}
	for i := 0; i < N; i++ {
		d := mix.Delay(rng, 1, 2, ct.KindCommit)
		switch {
		case d < 1*time.Millisecond:
			counts[0]++
		case d < 100*time.Millisecond:
			counts[1]++
		default:
			counts[2]++
		}
	}
	expect := []float64{0.25, 0.55, 0.20}
	for i, c := range counts {
		observed := float64(c) / float64(N)
		require.InDeltaf(t, expect[i], observed, 0.015,
			"component %d weight: expected %v, got %v (count %d / %d)", i, expect[i], observed, c, N)
	}
}

// TestLogNormalMixture_Slowed — Slowed(k) scales each component's
// median by k while keeping σ. The mixture's overall median should
// move by ~k as well (each component's median × k, weights unchanged).
func TestLogNormalMixture_Slowed(t *testing.T) {
	base := ct.Prod_1_2_3_4_CalibratedLogNormalMixture()
	slowed := base.Slowed(4)
	require.Len(t, slowed.Components, len(base.Components))
	for i, c := range base.Components {
		require.Equal(t, c.Sigma, slowed.Components[i].Sigma,
			"component %d sigma must be unchanged", i)
		require.Equal(t, c.Weight, slowed.Components[i].Weight,
			"component %d weight must be unchanged", i)
		require.InEpsilonf(t, float64(c.Median)*4, float64(slowed.Components[i].Median), 1e-9,
			"component %d median must scale by factor", i)
	}
}

// TestLogNormalMixture_HeavyTailed — HeavyTailed(k) scales every σ by
// the same binary-searched factor such that the mixture-level
// P(X > original-mixture-P99) grows by k. Asserted empirically on
// both a single-component mixture (where the per-component math is
// equivalent to the mixture math) and the calibrated prod mixture
// (where they diverge — the per-component formula would deliver ~1.8x
// at the mixture level, this test catches that regression).
func TestLogNormalMixture_HeavyTailed(t *testing.T) {
	cases := []struct {
		name string
		base *ct.LogNormalMixtureDelay
	}{
		{
			name: "single-component",
			base: ct.NewLogNormalMixtureDelay([]ct.LogNormalComponent{
				{Weight: 1, Median: 1000 * time.Microsecond, Sigma: 0.5},
			}),
		},
		{
			name: "prod-3-component",
			base: ct.Prod_1_2_3_4_CalibratedLogNormalMixture(),
		},
	}
	const (
		k = 4.0
		// Large sample so the P99-tail estimate is tight. SE of an
		// empirical probability p over N draws is √(p(1−p)/N); at
		// p≈0.04, N=200k gives SE ≈ 0.00044, i.e. ±0.0011 at 2.5σ.
		N = 200_000
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origP99Empirical := sampleMixtureP99(tc.base, 100_000, 13)
			heavy := tc.base.HeavyTailed(k)
			rng := mrand.New(mrand.NewSource(7))
			exceed := 0
			for i := 0; i < N; i++ {
				d := heavy.Delay(rng, 1, 2, ct.KindCommit)
				if d > origP99Empirical {
					exceed++
				}
			}
			observed := float64(exceed) / float64(N)
			// Expected = 1% × k = 4%. Tolerance ±0.5% absorbs the
			// compound P99-estimation error (±0.5% in V_99) and
			// sampling noise at p=0.04 (±0.11% at 2.5σ).
			require.InDeltaf(t, 0.04, observed, 0.005,
				"%s: P(X > original-P99=%v) after HeavyTailed(%v): expected 0.04, got %v (%d/%d)",
				tc.name, origP99Empirical, k, observed, exceed, N)
		})
	}

	// Structural sanity: HeavyTailed preserves Weight and Median for
	// every component; only Sigma changes.
	base := ct.Prod_1_2_3_4_CalibratedLogNormalMixture()
	heavyProd := base.HeavyTailed(k)
	require.Len(t, heavyProd.Components, len(base.Components))
	for i, c := range base.Components {
		require.Equal(t, c.Median, heavyProd.Components[i].Median,
			"component %d median must be unchanged", i)
		require.Equal(t, c.Weight, heavyProd.Components[i].Weight,
			"component %d weight must be unchanged", i)
		require.Greater(t, heavyProd.Components[i].Sigma, c.Sigma,
			"component %d sigma must grow (heavy-tailing widens, not narrows)", i)
	}
	// All components share the same uniform σ scale (heavy-tail invariant).
	scale0 := heavyProd.Components[0].Sigma / base.Components[0].Sigma
	for i := 1; i < len(base.Components); i++ {
		got := heavyProd.Components[i].Sigma / base.Components[i].Sigma
		require.InEpsilonf(t, scale0, got, 1e-9,
			"component %d sigma scale (%v) must match component 0 scale (%v)", i, got, scale0)
	}
}

// TestLogNormalMixture_MaxDelay — WithMaxDelay(d) clamps every draw at d
// (and d<=0 is a no-op default), while leaving sub-cap draws bit-for-bit
// identical. Guards the cap that heavy_tail / slow_heavy_tail rely on to
// keep their σ-fattened tails physical.
func TestLogNormalMixture_MaxDelay(t *testing.T) {
	base := ct.Prod_1_2_3_4_CalibratedLogNormalMixture().HeavyTailed(24) // huge uncapped tail
	const (
		cap = 1 * time.Second
		N   = 200_000
	)
	capped := base.WithMaxDelay(cap)

	// 1) No draw exceeds the cap, and some draws hit it (cap is active).
	hits := 0
	rng := mrand.New(mrand.NewSource(7))
	for i := 0; i < N; i++ {
		d := capped.Delay(rng, 1, 2, ct.KindCommit)
		require.LessOrEqualf(t, d, cap, "capped draw %v exceeds cap %v", d, cap)
		if d == cap {
			hits++
		}
	}
	require.Positivef(t, hits, "expected some draws to hit the %v cap on a heavy tail", cap)

	// 2) No cap (default, and d<=0) lets the tail exceed the cap.
	for _, m := range []*ct.LogNormalMixtureDelay{base, base.WithMaxDelay(0), base.WithMaxDelay(-1)} {
		over := 0
		r := mrand.New(mrand.NewSource(7))
		for i := 0; i < N; i++ {
			if m.Delay(r, 1, 2, ct.KindCommit) > cap {
				over++
			}
		}
		require.Positivef(t, over, "no-cap mixture should produce draws above %v", cap)
	}

	// 3) Exact semantics: capped draw == min(uncapped, cap) per sample
	// (WithMaxDelay only sets the bound, so the RNG stream is identical).
	rc := mrand.New(mrand.NewSource(11))
	ru := mrand.New(mrand.NewSource(11))
	for i := 0; i < N; i++ {
		dc := capped.Delay(rc, 1, 2, ct.KindCommit)
		du := base.Delay(ru, 1, 2, ct.KindCommit)
		want := du
		if want > cap {
			want = cap
		}
		require.Equalf(t, want, dc, "capped draw must equal min(uncapped, cap) at sample %d", i)
	}
}

// TestP2PProfiles_HeavyTailsCapped guards that heavy_tail and
// slow_heavy_tail keep their 5s WithMaxDelay bound wired in — without it
// the σ-fattened tail runs to 59s / DroppedDelay (1h), which the cap
// exists to prevent.
func TestP2PProfiles_HeavyTailsCapped(t *testing.T) {
	const (
		cap = 5 * time.Second
		N   = 300_000
	)
	for _, name := range []string{"heavy_tail", "slow_heavy_tail"} {
		m := ct.P2PProfile(name)
		rng := mrand.New(mrand.NewSource(7))
		for i := 0; i < N; i++ {
			d := m.Delay(rng, 1, 2, ct.KindCommit)
			require.LessOrEqualf(t, d, cap, "%s: draw %v exceeds the 5s cap", name, d)
		}
	}
}

// sampleMixtureP99 estimates the P99 of a mixture by drawing N samples
// and taking the 99th percentile. Empirical rather than analytic so
// the test doesn't import the internal mixtureQuantile helper.
func sampleMixtureP99(m *ct.LogNormalMixtureDelay, N int, seed int64) time.Duration {
	rng := mrand.New(mrand.NewSource(seed))
	samples := make([]time.Duration, N)
	for i := 0; i < N; i++ {
		samples[i] = m.Delay(rng, 1, 2, ct.KindCommit)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := int(0.99 * float64(N-1))
	return samples[idx]
}

// TestP2PProfile_AllNamesResolve — every name in P2PProfileNames must
// resolve to a usable NetworkModel; P2PProfileIndex must round-trip
// the name back to its index. Catches typos / mismatch between the
// switch in P2PProfile and the names list.
func TestP2PProfile_AllNamesResolve(t *testing.T) {
	require.NotEmpty(t, ct.P2PProfileNames)
	rng := mrand.New(mrand.NewSource(1))
	for i, name := range ct.P2PProfileNames {
		require.Equal(t, i, ct.P2PProfileIndex(name),
			"P2PProfileIndex(%q) must return the slice index", name)
		profile := ct.P2PProfile(name)
		require.NotNil(t, profile, "P2PProfile(%q) returned nil", name)
		// Sanity-sample to make sure Delay doesn't panic / return
		// negative durations.
		for j := 0; j < 100; j++ {
			d := profile.Delay(rng, 1, 2, ct.KindCommit)
			require.GreaterOrEqualf(t, d, time.Duration(0), "P2PProfile(%q): negative Delay %v", name, d)
		}
	}
	require.Panicsf(t,
		func() { ct.P2PProfile("nope-not-a-profile") },
		"P2PProfile should panic on unknown name")
	require.Panicsf(t,
		func() { ct.P2PProfileIndex("nope-not-a-profile") },
		"P2PProfileIndex should panic on unknown name")
}

// TestSlowOpAnchor_Synthetic — ConstantDelay returns D; LogNormalDelay
// returns 2×Median. Both preserve BTT-coupling for synthetic-BTT sweeps
// where the configured delay parameter IS the network-speed knob.
func TestSlowOpAnchor_Synthetic(t *testing.T) {
	require.Equal(t, 200*time.Millisecond,
		ct.ConstantDelay{D: 200 * time.Millisecond}.SlowOpAnchor())
	require.Equal(t, 300*time.Millisecond,
		ct.LogNormalDelay{Median: 150 * time.Millisecond, Sigma: 0.5}.SlowOpAnchor())
}

// TestSlowOpAnchor_Wrappers — every stateful wrapper (PerReceiver,
// Markovian, Partitioned, ClockSkewed, Lossy, Correlated) delegates to
// its inner model. A nested chain of wrappers must surface the
// innermost anchor unchanged. Catches a regression where any wrapper
// silently substitutes its own value (e.g. drop-cost as anchor).
func TestSlowOpAnchor_Wrappers(t *testing.T) {
	inner := ct.ConstantDelay{D: 200 * time.Millisecond}
	wrappers := []ct.NetworkModel{
		ct.PerReceiverDelay{Inner: inner},
		ct.NewMarkovianSlowness(inner, nil, 0, 0.5),
		ct.PartitionedNetwork{Inner: inner},
		ct.ClockSkewedNetwork{Inner: inner},
		ct.NewLossyNetwork(inner, 0.1, 5),
		ct.NewCorrelatedLinkDelay(inner, 0.1, 2, 5),
	}
	for i, w := range wrappers {
		require.Equalf(t, 200*time.Millisecond, w.SlowOpAnchor(),
			"wrapper[%d] (%T) must delegate SlowOpAnchor to inner", i, w)
	}
	// Doubly-nested: every layer should pass through.
	nested := ct.NewLossyNetwork(
		ct.NewMarkovianSlowness(
			ct.PerReceiverDelay{Inner: inner}, nil, 0, 0.5),
		0.1, 5)
	require.Equal(t, 200*time.Millisecond, nested.SlowOpAnchor(),
		"deeply-nested wrappers must still surface the innermost anchor")
}

// TestSlowOpAnchor_EmpiricalProfiles_PerProfileCalibration — each P2P
// profile returns its hand-tuned anchor independent of mixture median.
// Pinning the exact values here catches accidental anchor drift or
// constructor-rewiring regressions; the instability heatmap calibration
// depends on these specific numbers.
func TestSlowOpAnchor_EmpiricalProfiles_PerProfileCalibration(t *testing.T) {
	cases := []struct {
		profile string
		want    time.Duration
	}{
		{"prod", 250 * time.Millisecond},
		{"stage1", 250 * time.Millisecond},
		{"stage2", 250 * time.Millisecond},
		{"slow", 375 * time.Millisecond},
		{"heavy_tail", 300 * time.Millisecond},
		{"slow_heavy_tail", 450 * time.Millisecond},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, ct.P2PProfile(c.profile).SlowOpAnchor(),
			"profile %q SlowOpAnchor", c.profile)
	}
}

// TestSlowOpAnchor_MixtureFallback — a freshly-constructed mixture
// without an explicit anchor falls back to 2 × analytic median.
// Confirms the synthetic-mode default works for users who construct
// LogNormalMixtureDelay outside the calibrated empirical profiles.
func TestSlowOpAnchor_MixtureFallback(t *testing.T) {
	// Single-component mixture with median=10ms → expect anchor ≈ 20ms.
	mix := ct.NewLogNormalMixtureDelay([]ct.LogNormalComponent{
		{Weight: 1, Median: 10 * time.Millisecond, Sigma: 0.1},
	})
	got := mix.SlowOpAnchor()
	// mixtureQuantile is bisection-based; allow a small relative tolerance
	// rather than pinning the exact ns.
	require.InEpsilonf(t, float64(20*time.Millisecond), float64(got), 0.01,
		"single-component mixture: SlowOpAnchor ≈ 2×Median, got %v", got)
}

// TestSlowOpAnchor_MixtureOverride — WithSlowOpAnchor pins the value
// regardless of the underlying distribution.
func TestSlowOpAnchor_MixtureOverride(t *testing.T) {
	mix := ct.NewLogNormalMixtureDelay([]ct.LogNormalComponent{
		{Weight: 1, Median: 10 * time.Millisecond, Sigma: 0.1},
	}).WithSlowOpAnchor(500 * time.Millisecond)
	require.Equal(t, 500*time.Millisecond, mix.SlowOpAnchor(),
		"WithSlowOpAnchor must override the 2×Median fallback")
}

// TestSlowOpAnchor_DerivedMixturesDropAnchor — Slowed/HeavyTailed
// produce fresh mixtures with zero slowOpAnchor (the anchor is a
// calibration about the operating environment, not the network speed,
// so it doesn't propagate through scale transforms). P2PProfile is
// expected to set per-derived-profile anchors explicitly.
//
// Each derived mixture's SlowOpAnchor must equal the analytic fallback
// (2 × mixture median of the scaled distribution), pinned with a
// generous 1ms tolerance to absorb minor bisection / σ-search drift
// while still catching regressions. The pinned values come from the
// current Slowed/HeavyTailed algorithms; if either algorithm changes
// (e.g., HeavyTailed targets a different tail mass), update the
// expected numbers along with the algorithm change.
//
// Pinning catches three regression families:
//  1. Anchor propagation through Slowed/HeavyTailed (derived value
//     ≈ 250 ms — wildly outside either pinned target).
//  2. Wrong-fallback returning zero (derived value = 0 — outside).
//  3. Wrong-fallback returning some other value (would not match the
//     pin even if it differs from the source).
func TestSlowOpAnchor_DerivedMixturesDropAnchor(t *testing.T) {
	base := ct.Prod_1_2_3_4_CalibratedLogNormalMixture()
	require.Equal(t, 250*time.Millisecond, base.SlowOpAnchor(),
		"prod constructor sets anchor=250ms")

	// Slowed(80) scales every component median by 80; lognormal
	// scaling preserves the mixture median shape, so the analytic
	// fallback (2 × mixture median) ends up at ~226.69 ms — well below
	// the 250 ms source anchor and far from 0.
	slowed := base.Slowed(80)
	require.InDeltaf(t, float64(226690754), float64(slowed.SlowOpAnchor()), float64(time.Millisecond),
		"Slowed(80) fallback should match analytic 2×median ≈ 226.69ms, got %v", slowed.SlowOpAnchor())

	// HeavyTailed(24) scales sigmas (uniform σ-scale ≈ 2.0 at prod);
	// the mixture median shifts modestly because component medians are
	// untouched but the CDF reshapes. Analytic fallback lands at
	// ~2.98 ms — three orders of magnitude below the 250 ms source.
	heavy := base.HeavyTailed(24)
	require.InDeltaf(t, float64(2981946), float64(heavy.SlowOpAnchor()), float64(time.Millisecond),
		"HeavyTailed(24) fallback should match analytic 2×median ≈ 2.98ms, got %v", heavy.SlowOpAnchor())
}
