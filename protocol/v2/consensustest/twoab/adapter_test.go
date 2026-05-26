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

// TestAdapter_OpportunisticDecisionTime is the 2abOBFT sibling of the
// bare-OBFT test; asserts the observer-mode metric is active: under
// DeliveryDirect at BTT=200ms
// (ConstantDelay), σ-quorum at L_0 reaches via the KindValue-arrival
// observer path at TPhase2a + 1·BTT (KindValue carries the σ
// partial directly).
//
// Adapter derives:
//
//	resolveBudget = 2·BTT + ε_3 + jitter + HeaderSubmitHeadroom
//	              = 400 + 50 + 50 + 100 = 600ms
//	TPhase2a      = RelayCutoff − resolveBudget = 4000 − 600 = 3400ms
//
// (The 2·BTT reservation is for the worst-case L_0 → L_1 NR fall-through
// cascade; σ-quorum-at-L_0 only needs 1·BTT but the budget covers both.)
//
// Fastest path: TPhase2a fires at 3400ms → KindValue arrivals (with σ
// partial) at TPhase2a + 1·BTT = 3600ms → σ-pool[V_0] reaches qV at
// every honest receiver. tryOpportunisticResolve records vQuorumAt at
// the moment of qV. Reported DecisionTime preferentially reads
// vQuorumAt → 3600ms.
func TestAdapter_OpportunisticDecisionTime(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "decided at L_0 fastest path")
	// Async-fire: σ-eligible ops emit KindValue the moment their
	// L0Ready closes (bundle retained + host valid), NOT at the TPhase2a
	// backstop. Timeline at BTT=200ms, ConstantDelay{D=BTT}, SafetyBuffer=0:
	//   FetchAt[0] = T0Broadcast − B_0 = 3200 − 2·BTT = 2800ms (leader
	//     broadcasts its Phase-1 bundle here).
	//   bundle arrives at peers at 2800 + 1·BTT = 3000ms → L0Ready closes
	//     → peers async-fire KindValue (with σ partial) at 3000ms.
	//   peer KindValues arrive at 3000 + 1·BTT = 3200ms → σ-pool reaches
	//     qV → opportunistic Resolve decides at 3200ms.
	// This is TPhase2a − 1·BTT (3400 − 200), i.e. the decision lands
	// BEFORE the TPhase2a backstop. (SafetyBuffer=0 here, so the
	// resolveDeadline max-form is a no-op for this config — only
	// async-fire moves the time.)
	require.Equal(t, 3200*time.Millisecond, out.DecisionTime,
		"async-fire: L_0 σ-quorum at bundle-arrival + 1·BTT = 3200ms")
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

// TestAdapter_C1_QuorumBackedDecisionWiring mirrors the OBFT base
// adapter's same-named test. See obft/adapter_test.go for the
// rationale. Wiring test for Phase 2 C1.
func TestAdapter_C1_QuorumBackedDecisionWiring(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)
	require.Equal(t, 0, out.DecidedRound)

	att := out.CommitAttestation
	require.True(t, att.QuorumChecked, "QuorumChecked must be set under a healthy sim")
	require.Equal(t, 3, att.QuorumRequired, "QuorumRequired = 2f+1 = 3 at n=4")
	require.GreaterOrEqual(t, att.QuorumSigners, att.QuorumRequired,
		"QuorumSigners must be >= QuorumRequired under correct Resolve; got %d", att.QuorumSigners)

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.QuorumBackedDecision, "QuorumBackedDecision: %s", rep)
}

// TestAdapter_C3_HostValidityRecordingWiring mirrors the OBFT base
// adapter's same-named test. See obft/adapter_test.go for the
// rationale. Wiring test for Phase 2 C3.
func TestAdapter_C3_HostValidityRecordingWiring(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{4: true},
	}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest σ-emitters at L_0 → qV reached")

	att := out.CommitAttestation
	require.True(t, att.OBFTHostValidityChecked, "OBFTHostValidityChecked must be set on a decided sim")
	require.Equal(t, 0, att.OBFTHostValidityRejecters,
		"op4 had invalid verdict and correctly NV-emitted → 0 rejecters; got %d",
		att.OBFTHostValidityRejecters)

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.OBFTHostValidityRespect, "OBFTHostValidityRespect: %s", rep)
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

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }

// TestAdapter_Phase2EquivocateCrossV_FiresRule6a confirms that the new
// Phase-2 cross-V equivocation byz pattern actually drives the receiver-
// side Rule-6a detection path at the adapter / cluster level (not just at
// the unit-level Instance test). Verifies:
//   - Cluster decides (honest n-1 ops contribute to σ-quorum on V via
//     leader's broadcast and natural Phase-2a KindValue).
//   - At least one honest op records a Phase2Equivocation evidence fire
//     against the byz operator. Order tolerance: each honest receiver
//     might or might not see both ValueMsgs before its own decision (the
//     extra is scheduled with the same delay model as the natural one;
//     receivers fire Rule 6a on whichever arrives second).
func TestAdapter_Phase2EquivocateCrossV_FiresRule6a(t *testing.T) {
	profile := ct.CorrectnessProfile(200 * time.Millisecond)
	cfg := profile.BaseConfig
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzPhase2EquivocateCrossV,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "honest majority should still decide at L_0")
	var totalRule6a int
	for _, oo := range out.PerOp {
		totalRule6a += oo.EvidenceByRule[twoabadapter.RulePhase2Equivocation]
	}
	require.Greater(t, totalRule6a, 0,
		"at least one honest op should detect Rule 6a (Phase-2 equivocation) on the byz's cross-V double-emission")
}

// TestAdapter_Phase2DowngradeValueNoValue_FiresRule6a confirms the cross-
// KIND Phase-2 equivocation byz pattern actually drives the receiver-
// side Rule-6a detection path. Companion to the cross-V test; exercises
// the new universal-BuildExtra-hook path that allows the byz to inject
// extras of a kind different from the natural Phase-2a emission.
func TestAdapter_Phase2DowngradeValueNoValue_FiresRule6a(t *testing.T) {
	profile := ct.CorrectnessProfile(200 * time.Millisecond)
	cfg := profile.BaseConfig
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzPhase2DowngradeValueNoValue,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "honest majority should still decide at L_0")
	var totalRule6a int
	for _, oo := range out.PerOp {
		totalRule6a += oo.EvidenceByRule[twoabadapter.RulePhase2Equivocation]
	}
	require.Greater(t, totalRule6a, 0,
		"at least one honest op should detect Rule 6a (Phase-2 equivocation) on the byz's cross-kind downgrade sequence")
}

// TestAdapter_ResolveDeadline_SumToMax validates that
// the post-TPhase2a resolve window is
//
//	max(1·BTT + SafetyBuffer, 2·BTT) + ε_3 = 1·BTT + max(SafetyBuffer, 1·BTT) + ε_3
//
// (the `sum → max` revision), NOT the old `2·BTT + SafetyBuffer + ε_3`.
// The distinguishing, deterministic signature of the max-form is that
// the window is INSENSITIVE to SafetyBuffer below the 1·BTT crossover
// (the NR-side 2·BTT term dominates), and grows 1:1 with SafetyBuffer
// only above it. The old sum-form grew with SafetyBuffer everywhere.
//
// Decision-time relationship (healthy σ-path, ConstantDelay{D=BTT},
// async-fire): decision = TPhase2a − 1·BTT, and
// TPhase2a = RelayCutoff − (1·BTT + max(SB, 1·BTT) + ε_3 + jitter + headroom).
// So a larger window shifts TPhase2a (and thus the decision) EARLIER,
// while reclaiming MEV-fetch headroom at slot start (later FetchAt is the
// flip side of an earlier-anchored, longer post-TPhase2a window). The
// reclaim vs the old sum-form is min(1·BTT, SafetyBuffer); here we assert
// the max-form's SB-insensitivity below the crossover, which the sum-form
// could not exhibit.
//
// ConstantDelay{D=BTT} keeps the run deterministic.
func TestAdapter_ResolveDeadline_SumToMax(t *testing.T) {
	const btt = 200 * time.Millisecond
	run := func(sb time.Duration) ct.Outcome {
		cfg := ct.DefaultProposerDutyConfig(btt) // ConstantDelay{D=BTT} via Validate default
		out, err := twoabadapter.Protocol{SafetyBufferOverride: ptrDur(sb)}.Run(cfg)
		require.NoError(t, err)
		require.True(t, out.Decided, "healthy should decide at SB=%v", sb)
		require.Equal(t, 0, out.DecidedRound, "L_0 fastest path at SB=%v", sb)
		return out
	}

	outSB0 := run(0)
	outSB1 := run(btt)     // SB = 1·BTT — at the crossover
	outSB2 := run(2 * btt) // SB = 2·BTT — above the crossover

	// Below/at the crossover (SB ≤ 1·BTT), max(SB, 1·BTT) = 1·BTT, so the
	// window — and therefore TPhase2a and the decision time — are
	// IDENTICAL at SB=0 and SB=1·BTT. Under the OLD sum-form these would
	// differ by 1·BTT. This is the load-bearing max-vs-sum assertion.
	require.Equal(t, outSB0.DecisionTime, outSB1.DecisionTime,
		"max-form: SB below the 1·BTT crossover does not widen the window (NR-side 2·BTT dominates); sum-form would differ by 1·BTT")

	// Above the crossover, each extra BTT of SafetyBuffer widens the
	// window 1:1, shifting TPhase2a (and the decision) earlier by 1·BTT.
	require.Equal(t, outSB0.DecisionTime-btt, outSB2.DecisionTime,
		"max-form: SB=2·BTT widens the window by 1·BTT vs SB=0, decision earlier by 1·BTT")

	// Concrete anchors (BTT=200, RelayCutoff=4000, ε_3=50, jitter=50,
	// headroom=100): decision = RelayCutoff − 2·BTT − max(SB,BTT) − 200.
	require.Equal(t, 3200*time.Millisecond, outSB0.DecisionTime, "SB=0 decision")
	require.Equal(t, 3200*time.Millisecond, outSB1.DecisionTime, "SB=1·BTT decision (== SB=0)")
	require.Equal(t, 3000*time.Millisecond, outSB2.DecisionTime, "SB=2·BTT decision (1·BTT earlier)")

	t.Logf("decision times: SB=0 → %v, SB=1·BTT → %v, SB=2·BTT → %v",
		outSB0.DecisionTime, outSB1.DecisionTime, outSB2.DecisionTime)
}

// ptrDur returns a pointer to a time.Duration. Local helper for fields
// that take *time.Duration (e.g. Protocol.SafetyBufferOverride).
func ptrDur(d time.Duration) *time.Duration { return &d }

// TestAdapter_Equivocate_AllNR_JitterTradeoff tracks the accepted B1
// trade-off (see docs/2abOBFT.md §Phase 2a, Async fire on L0Ready): async-fire
// shrinks the equivocation-detection window, so under JITTERY delivery
// the Equivocate_AllNR scenario (byz leader floods both V_a and V_b to
// all honest ops) shifts from "always fall through to L_1" to "mostly
// decide fast at L_0, with a miss tail".
//
// This test is a REGRESSION TRACKER, not a pass/fail spec: it pins a
// conservative lower bound on the decision rate (so a future change that
// badly worsens the miss tail is caught) and — load-bearing — asserts
// SAFETY holds on EVERY run, including the misses (the trade-off is
// liveness-only). Under ConstantDelay the catalog still observes clean
// L_1 fall-through (TestCorrectness/Equivocate_AllNR/2abOBFT), so that
// path is covered elsewhere; here we deliberately use LogNormal.
func TestAdapter_Equivocate_AllNR_JitterTradeoff(t *testing.T) {
	const (
		btt   = 200 * time.Millisecond
		n     = 7
		seeds = 60
	)
	l0, l1, miss := 0, 0, 0
	for seed := int64(1); seed <= seeds; seed++ {
		cfg := ct.DefaultProposerDutyConfig(btt)
		cfg.N = n
		cfg.Operators = ct.MakeOperators(n)
		cfg.Seed = seed
		cfg.Network = ct.LogNormalDelay{Median: btt / 2, Sigma: 0.5}
		cfg.Byz = ct.ByzPattern{Kind: ct.ByzEquivocateAllNR, ByzOperators: []ct.OperatorID{1}}

		out, err := twoabadapter.Protocol{}.Run(cfg)
		require.NoError(t, err, "seed=%d", seed)

		// Safety MUST hold on every run, decided or missed — the B1
		// trade-off is liveness-only.
		rep := ct.ComputeSafetyReport(out)
		require.Truef(t, rep.SingleV, "seed=%d SingleV: %s", seed, rep)
		require.Truef(t, rep.NoOfflineDoubleV, "seed=%d NoOfflineDoubleV: %s", seed, rep)

		switch {
		case !out.Decided:
			miss++
		case out.DecidedRound == 0:
			l0++
		default:
			l1++
		}
	}
	decided := l0 + l1
	t.Logf("AllNR n=%d LogNormal, %d seeds: L0=%d L1=%d MISS=%d (decided=%d/%d)",
		n, seeds, l0, l1, miss, decided, seeds)

	// Conservative regression lower bound. Observed ~77% decided (all at
	// L_0) at the time of writing; assert ≥ 50% so a major worsening of
	// the miss tail trips the test, without flaking on seed-set drift.
	require.GreaterOrEqualf(t, decided, seeds/2,
		"AllNR decided rate regressed below 50%% under jitter (got %d/%d); the B1 trade-off worsened materially — re-evaluate async-fire",
		decided, seeds)
	// The decided cases resolve at L_0 (fast), not L_1: assert
	// the fast-path dominates the decided set (documents the distribution
	// shift, not just the rate).
	require.Greaterf(t, l0, l1,
		"expected L_0-fast to dominate decided AllNR runs with async-fire (got L0=%d L1=%d)", l0, l1)
}
