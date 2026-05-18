package twoab_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestMeshArrival_NoRefloodToPublisher mirrors the OBFT regression
// test. See its docstring for the design rationale.
func TestMeshArrival_NoRefloodToPublisher(t *testing.T) {
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
		TraceEnabled: true,
	}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	ct.AssertNoRefloodToPublisher(t, out.Trace)
}

// TestAdapter_OpportunisticDecisionTime — Phase 1 of the
// OBFT-OPPORTUNISTIC-PHASE3 plan, mirrored for 2abOBFT. Asserts the
// observer-mode metric is active: under DeliveryDirect at BTT=200ms
// (ConstantDelay), σ-quorum at L_0 reaches via the Onion2b-arrival
// observer path at T_commit + 1·BTT (vs pre-instrumentation
// RoundEndOffset). 2abOBFT's T_commit is later than OBFT's because of
// the Phase-2a Δ_2a budget: T_commit = RelayCutoff − HeaderSubmitHeadroom
// − phase3JitterBuffer − ε_3 − Δ_2a − Δ_2b. At BTT=200ms,
// Δ_2a=2·BTT=400ms (structural minimum per 2abOBFT spec) and
// Δ_2b=1·BTT=200ms (spec-aligned), so T_commit = 4000 − 100 − 50 − 50 −
// 400 − 200 = 3200ms; commits arrive at 3200 + 200 = 3400ms — well
// before RoundEndOffset = 3450ms.
func TestAdapter_OpportunisticDecisionTime(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "decided at L_0 fastest path")
	require.Equal(t, 3400*time.Millisecond, out.DecisionTime,
		"observer-mode Resolve should catch L_0 σ-quorum at T_commit + 1·BTT = 3400ms (was schedule-anchored 3450ms)")
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

// TestAdapter_BFTStart_BoundaryBehavior mirrors the OBFT test of the
// same name but anchors on `T_verdict_start = T_commit − Δ_2a` (the
// 2abOBFT-specific spec anchor at twoab/config.go §Setting) instead
// of T_commit directly. Three regimes:
//
//  1. BFTStart below the fetch-clamp boundary
//     (`tVerdictStart − B_0`) — clamp dormant; schedule and decision
//     time bit-identical to BFTStart=0.
//  2. BFTStart above the fetch-clamp boundary but below tVerdictStart
//     — clamp engages on L_0; primary's broadcast deadline floors at
//     BFTStart. Cluster still decides when the residual Phase-1
//     window fits propagation.
//  3. BFTStart ≥ tVerdictStart — no room for Phase-1 broadcast before
//     Phase-2a opens; cluster MISSes.
//
// 2abOBFT's anchor is earlier than OBFT's (tVerdictStart < tCommit by
// Δ_2a = 2·BTT), so the boundaries are tighter.
func TestAdapter_BFTStart_BoundaryBehavior(t *testing.T) {
	baseCfg := func(bft time.Duration) ct.SimConfig {
		c := ct.DefaultProposerDutyConfig(100 * time.Millisecond)
		c.N = 4
		c.Operators = ct.MakeOperators(4)
		c.K = 4
		c.Network = ct.P2PProfile("prod")
		c.Mesh.HopDelay = ct.P2PProfile("prod")
		c.BFTStart = bft
		return c
	}

	// At BTT=100ms, K=4, RefloodDelay=0 (default), spec sizing:
	// tCommit = 4000 − 100 − 50 − 50 − 300 = 3500ms; Δ_2a = 2·BTT = 200ms
	// → tVerdictStart = 3500 − 200 = 3300ms; B_0 = 2·BTT = 200ms
	// → fetch-clamp boundary = tVerdictStart − B_0 = 3100ms.
	// DecidingBroadcastTime clamp engages at the same threshold (no
	// fetchBuffer subtraction in 2abOBFT, unlike OBFT).

	// Case 1: BFTStart well below the clamp boundary — bit-identical to BFT=0.
	cfg0 := baseCfg(0)
	out0, err := twoabadapter.Protocol{}.Run(cfg0)
	require.NoError(t, err)
	require.True(t, out0.Decided, "BFTStart=0 must decide at healthy mesh")

	for _, bft := range []time.Duration{500, 1600, 2400, 3000} {
		cfgLow := baseCfg(bft * time.Millisecond)
		outLow, err := twoabadapter.Protocol{}.Run(cfgLow)
		require.NoErrorf(t, err, "BFTStart=%dms run", bft)
		require.Truef(t, outLow.Decided, "BFTStart=%dms (< clamp boundary) must decide", bft)
		require.Equalf(t, out0.DecisionTime, outLow.DecisionTime,
			"BFTStart=%dms < clamp boundary must produce bit-identical DecisionTime", bft)
		require.Equalf(t, out0.DecidingBroadcastTime, outLow.DecidingBroadcastTime,
			"BFTStart=%dms < clamp boundary must produce bit-identical DecidingBroadcastTime", bft)
	}

	// Case 2: BFTStart above L_0 fetch-clamp boundary but below
	// tVerdictStart — clamp engages; DecidingBroadcastTime floors to
	// BFTStart; cluster still decides because the residual Phase-1
	// window (tVerdictStart − BFTStart) fits propagation.
	cfgClamped := baseCfg(3200 * time.Millisecond)
	outClamped, err := twoabadapter.Protocol{}.Run(cfgClamped)
	require.NoError(t, err)
	require.True(t, outClamped.Decided,
		"BFTStart=3200ms (clamp engaged, 100ms residual Phase-1 window) must still decide on prod profile")
	require.GreaterOrEqual(t, outClamped.DecidingBroadcastTime, 3200*time.Millisecond,
		"BFTStart=3200ms must floor DecidingBroadcastTime to ≥ BFTStart per the spec clamp")

	// Case 3: BFTStart ≥ tVerdictStart — no Phase-1 window left;
	// cluster MISSes.
	cfgPastVerdict := baseCfg(3400 * time.Millisecond)
	outPastVerdict, err := twoabadapter.Protocol{}.Run(cfgPastVerdict)
	require.NoError(t, err)
	require.False(t, outPastVerdict.Decided,
		"BFTStart=3400ms ≥ tVerdictStart must MISS (no Phase-1 window before verdict opens)")
	require.NotEmpty(t, outPastVerdict.MissReason,
		"miss must carry a reason for the failure-breakdown table")
}

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }
