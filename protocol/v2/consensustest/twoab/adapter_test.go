package twoab_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestAdapter_OpportunisticDecisionTime — Phase 1 of the
// OBFT-OPPORTUNISTIC-PHASE3 plan, mirrored for 2abOBFT. Asserts the
// observer-mode metric is active: under DeliveryDirect at BTT=200ms
// (ConstantDelay), σ-quorum at L_0 reaches via the Onion2b-arrival
// observer path at T_commit + 1·BTT (vs pre-instrumentation
// RoundEndOffset). 2abOBFT's T_commit is later than OBFT's because of
// the Phase-2a Δ_2a budget: T_commit = RelayCutoff − HeaderSubmitHeadroom
// − phase3JitterBuffer − ε_3 − Δ_2a − Δ_2b. At BTT=200ms, Δ_2a=Δ_2b=400ms,
// so T_commit = 4000 − 100 − 50 − 50 − 400 − 400 = 3000ms; commits arrive
// at 3000 + 200 = 3200ms — well before RoundEndOffset = 3450ms.
func TestAdapter_OpportunisticDecisionTime(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "decided at L_0 fastest path")
	require.Equal(t, 3200*time.Millisecond, out.DecisionTime,
		"observer-mode Resolve should catch L_0 σ-quorum at T_commit + 1·BTT = 3200ms (was schedule-anchored 3450ms)")
}

// TestAdapter_HealthyMesh_N4 — 2abOBFT healthy through the mesh
// transport. See the OBFT adapter's mesh smoke for the rationale.
func TestAdapter_HealthyMesh_N4(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            4,
		Operators:    ct.MakeOperators(4),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz:          ct.ByzPattern{Kind: ct.ByzNone},
		Seed:         1,
		Delivery:     ct.DeliveryMesh,
		Mesh: ct.MeshConfig{
			HopDelay: ct.LogNormalDelay{Median: btt / 3, Sigma: 0.3},
		},
	}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "mesh-mode healthy should decide at L_0 fastest path")
	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("mesh-mode healthy: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_HealthyAtClusterSizes verifies the adapter runs healthy at
// every SSV-supported cluster size (n=4, 7, 10, 13). Mirrors the bare
// OBFT adapter's TestAdapter_HealthyAtClusterSizes.
func TestAdapter_HealthyAtClusterSizes(t *testing.T) {
	btt := 200 * time.Millisecond
	for _, n := range ct.ClusterSizes {
		n := n
		t.Run(clusterName(n), func(t *testing.T) {
			cfg := ct.SimConfig{
				N:            n,
				Operators:    ct.MakeOperators(n),
				SlotDuration: 12 * time.Second,
				RelayCutoff:  4 * time.Second,
				BTT:          btt,
				Byz:          ct.ByzPattern{Kind: ct.ByzNone},
				Seed:         1,
			}
			out, err := twoabadapter.Protocol{}.Run(cfg)
			require.NoError(t, err, "n=%d Run", n)
			require.True(t, out.Decided, "n=%d should decide healthy", n)
			require.Equal(t, 0, out.DecidedRound, "n=%d should decide at L_0 fastest path", n)

			rep := ct.ComputeSafetyReport(out)
			require.True(t, rep.SingleV, "n=%d SingleV: %s", n, rep)
			require.True(t, rep.NoOfflineDoubleV, "n=%d NoOfflineDoubleV: %s", n, rep)
			t.Logf("n=%d K=%d: decided at %v on L_%d", n, ct.DefaultK(cfg.N), out.DecisionTime, out.DecidedRound)
		})
	}
}

// TestAdapter_CatalogRunsToCompletion verifies every catalog scenario runs
// to a defined outcome (Decided or Err non-empty) without panicking. This
// is the K4 smoke test — its purpose is to surface translation bugs in
// the adapter, NOT to assert per-scenario expectations (that's
// TestCorrectness in the framework's tier suite).
func TestAdapter_CatalogRunsToCompletion(t *testing.T) {
	profile := ct.CorrectnessProfile(200 * time.Millisecond)
	for _, s := range ct.Catalog {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			cfg := profile.BaseConfig
			s.Apply(&cfg)
			out, err := twoabadapter.Protocol{}.Run(cfg)
			if err == ct.ErrNotApplicable {
				t.Skip("scenario not applicable to 2abOBFT")
			}
			require.NoError(t, err, "Run")
			// Safety invariants must hold even if the scenario classifies as MISS.
			rep := ct.ComputeSafetyReport(out)
			require.True(t, rep.SingleV, "%s SingleV: %s", s.Name, rep)
			require.True(t, rep.NoOfflineDoubleV, "%s NoOfflineDoubleV: %s", s.Name, rep)
			// Log the outcome for use in K5 (filling 2abOBFT Expect entries).
			if out.Decided {
				t.Logf("  → DECIDED at L_%d, %v", out.DecidedRound, out.DecisionTime)
			} else {
				t.Logf("  → MISS")
			}
		})
	}
}

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }
