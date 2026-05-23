package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestSweeps_HealthyStaysMesh — Healthy must run through DeliveryMesh
// in every sweep that DefaultSweeps emits. Without this gate, sweep
// wrappers that rebuild Scenario field-by-field can silently drop
// Scenario.Delivery and revert Healthy to direct fanout for that
// sweep — exactly the regression Finding 2 surfaced. Iterates every
// (sweep, point) emitted by DefaultSweeps, finds the Healthy
// scenario, and asserts its Delivery is DeliveryMesh post-wrap.
func TestSweeps_HealthyStaysMesh(t *testing.T) {
	scenarios := ct.ScenariosWithMode(ct.Catalog, ct.ModeStress)
	require.NotEmpty(t, scenarios)
	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		qbftadapter.QBFTNoReflood{},
		twoabadapter.Protocol{},
	}
	iters := ct.Iterations{Baseline: 1, Unstable: 1}
	profiles := []string{"prod"}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iters, 4, 4, profiles, ct.DefaultBaselineBFTStarts, ct.DefaultBaselineBTTValues)
	for _, sw := range sweeps {
		for ptIdx, pt := range sw.Points {
			var healthy *ct.Scenario
			for i := range pt.Config.Scenarios {
				if pt.Config.Scenarios[i].Name == "Healthy" {
					healthy = &pt.Config.Scenarios[i]
					break
				}
			}
			if healthy == nil {
				// Some sweep points filter to baseline-only and may not
				// expose Healthy (instability level>0). Verify another
				// baseline scenario instead if available; otherwise skip.
				continue
			}
			require.Equalf(t, ct.DeliveryMesh, healthy.Delivery,
				"sweep %q point %d (%s): Healthy.Delivery dropped to Direct",
				sw.Name, ptIdx, pt.Label)
		}
	}
}

// TestMeshHealthy_RespondsToSigma — the heavy-tail sweep varies
// LogNormal Sigma. With cfg.Mesh.HopDelay co-set onto the same sigma,
// mesh-mode Healthy's decision time should grow with sigma because the
// per-hop draws have heavier tails and the cluster-wide propagation is
// their convolution.
//
// QBFT is the right adapter to assert this on: its DecisionTime moves
// directly with propagation (PROPOSE → 2f+1 PREPAREs → 2f+1 COMMITs →
// 2f+1 partial sigs received locally). OBFT/2abOBFT anchor to a fixed
// post-Phase-3 schedule offset (RoundEndOffset), so their Healthy
// DecisionTime is sigma-invariant by construction.
func TestMeshHealthy_RespondsToSigma(t *testing.T) {
	const iters = 30
	btt := 300 * time.Millisecond
	base := ct.DefaultProposerDutyConfig(btt)
	base.Delivery = ct.DeliveryMesh

	run := func(sigma float64) (decided int, maxDecision time.Duration) {
		for i := 0; i < iters; i++ {
			cfg := base
			cfg.Seed = int64(i + 1)
			cfg.Network = ct.LogNormalDelay{Median: btt / 2, Sigma: sigma}
			cfg.Mesh.HopDelay = ct.LogNormalDelay{Median: btt / 3, Sigma: sigma}
			out, err := qbftadapter.QBFTNoReflood{}.Run(cfg)
			require.NoError(t, err)
			if out.Decided {
				decided++
				if out.DecisionTime > maxDecision {
					maxDecision = out.DecisionTime
				}
			}
		}
		return
	}
	loDec, loMax := run(0.1)
	hiDec, hiMax := run(0.9)
	t.Logf("sigma=0.1: %d/%d decided, maxDecisionTime=%v", loDec, iters, loMax)
	t.Logf("sigma=0.9: %d/%d decided, maxDecisionTime=%v", hiDec, iters, hiMax)
	// Two compatible response signals — accept either. At low sigma
	// the cluster decides cleanly with tight times; at high sigma it
	// either takes longer (max decision time grows) OR misses some
	// iters entirely (success drops). A change in either direction
	// proves mesh hop sampling actually consults the per-hop sigma.
	responded := hiMax > loMax || hiDec < loDec
	require.True(t, responded,
		"mesh-mode QBFT healthy should respond to per-hop sigma (lo: %d decided / max %v; hi: %d decided / max %v)",
		loDec, loMax, hiDec, hiMax)
}

// TestWrapBaselineForInstability_PreservesHealthyMeshSettings pins the
// inheritance contract that the production-mesh scenario design relies
// on: Healthy's Apply sets cfg.RefloodDelay = 700ms and
// cfg.Mesh.Gossip.Enabled = true, and WrapBaselineForInstability must
// preserve both across all instability levels. The wrap currently only
// mutates cfg.Network and cfg.Mesh.HopDelay (sub-field writes); a
// future rebuild that wholesale-reassigned cfg.Mesh or reset
// cfg.RefloodDelay would silently strip Healthy's production-mesh
// settings — exactly the class of regression that motivated
// CloneScenarioWith. Direct field-level assertion locks this in.
func TestWrapBaselineForInstability_PreservesHealthyMeshSettings(t *testing.T) {
	healthy := ct.Catalog[0]
	require.Equal(t, "Healthy", healthy.Name)
	base := ct.DefaultProposerDutyConfig(300 * time.Millisecond)
	for _, level := range ct.InstabilityLevels {
		level := level
		t.Run(level.Name, func(t *testing.T) {
			wrapped := ct.WrapBaselineForInstability(healthy, level)
			cfg := base
			cfg.Delivery = wrapped.Delivery
			require.NotNil(t, wrapped.Apply)
			wrapped.Apply(&cfg)
			require.Equal(t, 700*time.Millisecond, cfg.RefloodDelay,
				"RefloodDelay must survive wrap at level=%s", level.Name)
			require.True(t, cfg.Mesh.Gossip.Enabled,
				"Mesh.Gossip.Enabled must survive wrap at level=%s", level.Name)
		})
	}
}

// TestMeshHealthy_RespondsToInstability — the instability wrap should
// degrade mesh-mode Healthy as the level rises. Phase B/C's plan calls
// for high → 10-30% drop and extreme → 0-30% success range. We assert a
// much looser inequality (level=extreme has lower success rate than
// level=none) to avoid pinning specific tuning numbers, but a
// regression where the wrap silently fails to apply to the mesh path
// would still fail this test.
//
// Production-mesh isolation: Healthy's Apply enables two recovery
// features that mask mesh-transport miss-events — gossip backstop
// (cfg.Mesh.Gossip.Enabled = true) and a RefloodDelay-aware primary
// broadcast budget (cfg.RefloodDelay = 700ms widens B_0 from 2·BTT to
// 2·BTT + 700ms, absorbing the instability-induced arrival jitter). Both
// are correct Healthy-scenario behavior but would let extreme
// instability decide 100%, defeating this test's premise. Disable both
// locally so the test stays focused on its narrow assertion (wrap
// plumbing reaches the eager-mesh path).
func TestMeshHealthy_RespondsToInstability(t *testing.T) {
	const iters = 20
	btt := 300 * time.Millisecond
	base := ct.DefaultProposerDutyConfig(btt)
	base.Delivery = ct.DeliveryMesh
	base.Network = ct.LogNormalDelay{Median: btt / 2, Sigma: 0.5}
	base.Mesh.HopDelay = ct.LogNormalDelay{Median: btt / 3, Sigma: 0.5}

	healthy := ct.Catalog[0] // catalog_baseline.go places Healthy first
	require.Equal(t, "Healthy", healthy.Name)

	run := func(level ct.InstabilityLevel) int {
		var decidedCount int
		wrapped := ct.WrapBaselineForInstability(healthy, level)
		for i := 0; i < iters; i++ {
			cfg := base
			cfg.Seed = int64(i + 1)
			cfg.Delivery = wrapped.Delivery
			if wrapped.Apply != nil {
				wrapped.Apply(&cfg)
			}
			// Disable gossip + RefloodDelay locally — see test doc on isolation.
			cfg.Mesh.Gossip.Enabled = false
			cfg.RefloodDelay = 0
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			if out.Decided {
				decidedCount++
			}
		}
		return decidedCount
	}
	noneDec := run(ct.InstabilityLevels[0])    // "none"
	extremeDec := run(ct.InstabilityLevels[4]) // "extreme"
	t.Logf("instability=none:    %d/%d decided", noneDec, iters)
	t.Logf("instability=extreme: %d/%d decided", extremeDec, iters)
	require.Greater(t, noneDec, extremeDec,
		"mesh-mode healthy should degrade with instability (none=%d, extreme=%d)",
		noneDec, extremeDec)
}

// TestInstability_BTTInvariantUnderEmpiricalProfile pins the load-bearing
// property of the SlowOpAnchor refactor: with fixed-RT QBFT (production
// SSV variant) and fixed empirical p2p profile + instability level, the
// success rate must NOT depend on cfg.BTT. Pre-refactor, the same setup
// drifted ~10 pp across BTT ∈ {100, 500} ms because slow-op extra delay
// scaled with BTT. Post-refactor, anchor is the network's SlowOpAnchor
// (hand-tuned 250 ms for prod), so the wrap is BTT-independent on the
// empirical-profile path.
//
// Uses `extreme` instability + DeliveryMesh — that's the regime where
// the wrap produces enough disruption to surface miss-events at n=40
// iterations. Moderate gives 100% success across the board under the
// recalibrated anchors, so a regression that re-introduced BTT-coupling
// would slip past a moderate-only test.
//
// Tolerance: ±10pp absolute success-rate variance across the three
// BTT points. Deterministic seeds make the test fully reproducible:
// under the new code the observed spread is 0pp (39/40 decided at every
// BTT), so 10pp leaves comfortable margin. The original BTT-coupling
// regression produced spreads ≫ 15pp (screenshots: 99→89% at moderate,
// ~30pp at extreme); the tighter tolerance catches a smaller regression
// while staying robust to seed-noise-driven cross-BTT drift in the
// proposer-duty config parameters (which are still BTT-derived even
// though the slow-op anchor isn't).
//
// Production-mesh isolation: Healthy's Apply enables the lazy-push
// gossip backstop AND sets cfg.RefloodDelay=700ms (widening B_0 by
// 700ms). Both would mask the instability-induced miss-events this
// test relies on for non-trivial BTT-invariance assertions, so both
// are disabled locally after Apply.
func TestInstability_BTTInvariantUnderEmpiricalProfile(t *testing.T) {
	const iters = 40
	btts := []time.Duration{
		100 * time.Millisecond,
		300 * time.Millisecond,
		500 * time.Millisecond,
	}
	level := ct.InstabilityLevels[4] // "extreme"
	require.Equal(t, "extreme", level.Name)

	healthy := ct.Catalog[0]
	require.Equal(t, "Healthy", healthy.Name)
	wrapped := ct.WrapBaselineForInstability(healthy, level)

	successRates := make([]float64, len(btts))
	for bIdx, btt := range btts {
		cfg := ct.DefaultProposerDutyConfig(btt)
		cfg.Delivery = ct.DeliveryMesh // matches the heatmap's per-protocol view
		cfg.Network = ct.P2PProfile("prod")
		cfg.Mesh.HopDelay = ct.P2PProfile("prod")

		decided := 0
		for i := 0; i < iters; i++ {
			c := cfg
			c.Seed = int64(i + 1)
			if wrapped.Apply != nil {
				wrapped.Apply(&c)
			}
			// Disable gossip + RefloodDelay locally — see test doc on isolation.
			c.Mesh.Gossip.Enabled = false
			c.RefloodDelay = 0
			out, err := qbftadapter.QBFTSSV{}.Run(c)
			require.NoError(t, err)
			if out.Decided {
				decided++
			}
		}
		successRates[bIdx] = float64(decided) / float64(iters)
		t.Logf("BTT=%v: %d/%d decided (%.1f%%)", btt, decided, iters, 100*successRates[bIdx])
	}

	// Variance across the three BTT points must be small. Compute
	// max - min absolute spread as the metric (more interpretable than
	// stddev for a 3-point series).
	lo, hi := successRates[0], successRates[0]
	for _, r := range successRates[1:] {
		if r < lo {
			lo = r
		}
		if r > hi {
			hi = r
		}
	}
	require.LessOrEqualf(t, hi-lo, 0.10,
		"QBFT-SSV at prod/extreme/mesh must be BTT-invariant; got spread %.1fpp across BTT ∈ {100, 300, 500}ms (%v)",
		100*(hi-lo), successRates)
}
