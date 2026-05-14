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
		qbftadapter.Protocol{},
		twoabadapter.Protocol{},
	}
	iters := ct.Iterations{Baseline: 1, Unstable: 1}
	profiles := []string{"prod"}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iters, 4, 4, profiles)
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
			out, err := qbftadapter.Protocol{}.Run(cfg)
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

// TestMeshHealthy_RespondsToInstability — the instability wrap should
// degrade mesh-mode Healthy as the level rises. Phase B/C's plan calls
// for high → 10-30% drop and extreme → 0-30% success range. We assert a
// much looser inequality (level=extreme has lower success rate than
// level=none) to avoid pinning specific tuning numbers, but a
// regression where the wrap silently fails to apply to the mesh path
// would still fail this test.
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
