package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestBandwidth_Healthy_OBFT verifies the OBFT adapter populates
// Outcome.Bandwidth with non-zero per-kind / per-operator counts on a
// healthy slot. Asserts per-kind cluster-wide bands (loose around
// observed values at BTT=200, K=4, n=4) so single-component regressions
// surface here — a 30% growth in any kind would fail the test, while
// format-evolution churn under 15% stays green.
//
// V (consensus value) is sized to a realistic Electra blinded block
// (~5 KB; see ct.RealisticBlindedBlockBytes). OBFT's per-commit V
// replication across σ-state layers — (K-1) × V bytes per commit in the
// K=N=4 healthy path — makes Phase-2 the dominant cost; Certificate
// gossip and Phase-1 bundle add 1 × V per peer-pair each.
//
// Forces K=N=4 (up-tier) regardless of the K=f+1 BFT-min default so the
// bandwidth bands stay anchored to the V-replication-dominated cost
// model documented above. A separate K=2 bandwidth test could validate
// the smaller-onion default; this test guards the K=N upper-bound surface.
func TestBandwidth_Healthy_OBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.K = cfg.N // up-tier — bandwidth bands are K=N-anchored
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)

	require.Greater(t, out.Bandwidth.TotalBytes, int64(0),
		"healthy OBFT must record non-zero bandwidth")
	// Healthy-path total at V≈5 KB n=4 K=4: ~320 KB cluster-wide. Loose
	// upper bound at 400 KB catches any major regression while leaving
	// room for spec-format evolution under ±25%.
	require.Less(t, out.Bandwidth.TotalBytes, int64(400*1024),
		"healthy OBFT at n=4 V≈5 KB should fit under 400 KB; got %s", out.Bandwidth.SummaryLine())

	// Per-kind sanity: leader broadcasts at K=4 layers + commits at every op +
	// cert-gossip after Phase 3 reconstruction.
	require.Greater(t, out.Bandwidth.PerKindBytes["LeaderBroadcast"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Commit"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Certificate"], int64(0),
		"healthy OBFT should dispatch cert gossip after reconstruction")

	// Per-kind bands (cluster-wide, sim observed at BTT=200 K=4 n=4 stub
	// crypto with V≈5 KB realistic Electra blinded block). Bands sit at
	// observed ±15-20%; a regression that doubles witness/onion size or
	// changes the V-replication factor would shift the affected kind out
	// of band.
	type bandCheck struct {
		kind string
		min  int64
		max  int64
	}
	bands := []bandCheck{
		// LeaderBroadcast: K=4 leaders × (N-1)=3 peers × ~5300 B bundle
		// (V(~5152) + ClusterID(32) + OpID(8) + Height(8) + Layer(4) +
		// LeaderSigma(96)) ≈ 62 KB. Observed ~62.1 KB.
		{"LeaderBroadcast", 55000, 70000},
		// Commit: N=4 ops × (N-1)=3 peers × ~16 KB commit body ≈ 193 KB.
		// Per-commit body: base(48) + (K-1)=3 σ-state onion entries with
		// V(~5152) + Ciphertext(96 σ + 48 IBE for k>0) ≈ ~3×5300 = 15.9 KB,
		// plus K=4 witnesses (145 B each = 580 B). Cluster total ~190 KB.
		// Phase-2's V-replication across σ-state layers is the dominant
		// OBFT cost at realistic V — observed ~193.1 KB.
		{"Commit", 170000, 220000},
		// Certificate: N=4 ops × (N-1)=3 peers × ~5300 B cert body
		// (V(~5152) + ClusterID(32) + Height(8) + Sig(96)) ≈ 62 KB.
		// Observed ~62.0 KB.
		{"Certificate", 55000, 70000},
	}
	for _, b := range bands {
		got := out.Bandwidth.PerKindBytes[b.kind]
		require.GreaterOrEqualf(t, got, b.min,
			"%s bandwidth %d B below expected min %d B (regression?); summary: %s",
			b.kind, got, b.min, out.Bandwidth.SummaryLine())
		require.LessOrEqualf(t, got, b.max,
			"%s bandwidth %d B above expected max %d B (regression?); summary: %s",
			b.kind, got, b.max, out.Bandwidth.SummaryLine())
	}

	// Per-operator: every op both sends and receives in healthy round.
	// Per-op average outgoing should be ~total/N = ~4 KB at n=4 (every op
	// emits LeaderBroadcast + Commit + Cert at K=N convention).
	for op, oo := range out.PerOp {
		require.Greaterf(t, oo.BandwidthOut, int64(0),
			"op=%d should send bytes in healthy round", op)
		require.Greaterf(t, oo.BandwidthIn, int64(0),
			"op=%d should receive bytes in healthy round", op)
	}

	t.Logf("OBFT %s", out.Bandwidth.SummaryLine())
}

// TestBandwidth_Healthy_QBFT verifies the QBFT adapter populates Bandwidth
// from inner-only SSVMessage encoding (apples-to-apples with OBFT — see
// consensustest/qbft/network.go messageWireBytes). PROPOSE + PREPARE +
// COMMIT + post-consensus partial-sigs produce distinct per-kind buckets
// at canonical config.
func TestBandwidth_Healthy_QBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)

	require.Greater(t, out.Bandwidth.TotalBytes, int64(0),
		"healthy QBFT must record non-zero bandwidth")

	// PROPOSE = LeaderBroadcast; PREPARE = Prepare; COMMIT = Commit
	// (the three QBFT consensus phases live in distinct buckets so the
	// per-kind chart doesn't conflate Phase-2 PREPARE with Phase-3
	// COMMIT bytes — see consensustest/network.go MsgKind for the split
	// rationale).
	require.Greater(t, out.Bandwidth.PerKindBytes["LeaderBroadcast"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Prepare"], int64(0),
		"healthy QBFT should dispatch PREPARE under its own bucket")
	require.Greater(t, out.Bandwidth.PerKindBytes["Commit"], int64(0))
	// SSV's QBFT-then-post-consensus model: each honest decided op
	// broadcasts one PartialSignatureMessage. At n=4, expect 4 × 3 × ~233 B
	// = ~2.8 KB of post-consensus traffic.
	require.Greater(t, out.Bandwidth.PerKindBytes["PostConsensus"], int64(0),
		"healthy QBFT should dispatch post-consensus partial-sigs after decided")

	for op, oo := range out.PerOp {
		require.Greaterf(t, oo.BandwidthOut, int64(0),
			"op=%d should send bytes in healthy round", op)
		require.Greaterf(t, oo.BandwidthIn, int64(0),
			"op=%d should receive bytes in healthy round", op)
	}

	t.Logf("QBFT %s", out.Bandwidth.SummaryLine())
}

// TestBandwidth_QBFT_RoundChange_AddsRoundChangeBytes verifies that on
// round-change (R1 silent → R2 success), the QBFT adapter records non-zero
// RoundChange bytes — round-1 healthy has zero. Confirms the round-change
// message kind is dispatched and accounted.
func TestBandwidth_QBFT_RoundChange_AddsRoundChangeBytes(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}

	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "QBFT R1 silent → R2 should decide")
	require.Greater(t, out.Bandwidth.PerKindBytes["RoundChange"], int64(0),
		"R2 path must dispatch ROUND_CHANGE messages: %s", out.Bandwidth.SummaryLine())
}

// TestBandwidth_QBFT_ByzProposalCounted verifies byz-fabricated PROPOSEs are
// charged to bandwidth. Equivocate111 fabricates 1 PROPOSE per non-leader
// honest at round 1; the byz leader contributes only via the byz dispatch
// path (no Instance, no virtualNetwork.Broadcast), so without this accounting
// the round-1 byz PROPOSE bytes would silently disappear from the report.
func TestBandwidth_QBFT_ByzProposalCounted(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzEquivocate111, ByzOperators: []ct.OperatorID{1}}

	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)

	// Honest leader at R2 (op2) emits one PROPOSE via virtualNetwork.Broadcast;
	// byz leader at R1 (op1) fabricates 3 distinct PROPOSEs (one per honest)
	// via evtByzProposal. Op1 should appear in PerOperatorOut even though it
	// has no Instance — that's only true if byz PROPOSE bytes are charged.
	require.Greater(t, out.Bandwidth.PerOperatorOut[1], int64(0),
		"byz leader op1 should have non-zero out-bytes from R1 PROPOSE fabrication: %s",
		out.Bandwidth.SummaryLine())
}
