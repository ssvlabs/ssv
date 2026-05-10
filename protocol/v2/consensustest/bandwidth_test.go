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
// healthy slot. Asserts per-kind cluster-wide bands (loose ±25% around
// observed values at BTT=200, K=4, n=4) so single-component regressions
// surface here — a 50% growth in any kind would fail the test, while
// format-evolution churn under 25% stays green.
//
// Spec referent: OBFT.md §Properties summary quotes ~28 KB cluster-wide
// at K=4 n=4. The sim's stub-BLS / stub-IBE numbers are lower (no SSV
// outer auth envelope, simpler IBE overhead) — we band against the sim's
// own expected sizes computed in obft/sizes.go, not the spec's
// production-envelope quote.
func TestBandwidth_Healthy_OBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)

	require.Greater(t, out.Bandwidth.TotalBytes, int64(0),
		"healthy OBFT must record non-zero bandwidth")
	require.Less(t, out.Bandwidth.TotalBytes, int64(30*1024),
		"healthy OBFT at n=4 should fit under 30 KB; got %s", out.Bandwidth.SummaryLine())

	// Per-kind sanity: leader broadcasts at K=4 layers + commits at every op +
	// cert-gossip after Phase 3 reconstruction.
	require.Greater(t, out.Bandwidth.PerKindBytes["LeaderBroadcast"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Commit"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Certificate"], int64(0),
		"healthy OBFT should dispatch cert gossip after reconstruction")

	// Per-kind bands (cluster-wide, sim observed at BTT=200 K=4 n=4 stub
	// crypto; loose ±25% so message-format evolution stays green within a
	// quarter without false positives, but single-component 50% bloats
	// fail). The bands check the bandwidth shape, not just totals — a
	// regression that doubles witness size would shift Commit out of band.
	type bandCheck struct {
		kind string
		min  int64
		max  int64
	}
	bands := []bandCheck{
		// LeaderBroadcast: K=4 leaders × (N-1)=3 recipients × ~163 B body
		// ≈ 1956 B. Stub Phase-1 bundle = ClusterID(32) + OpID(8) + Height(8)
		// + Layer(4) + V(~15) + SigmaV(96) = 163 B.
		{"LeaderBroadcast", 1500, 2500},
		// Commit: N=4 ops × (N-1)=3 recipients × ~1040 B body ≈ 12500 B.
		// Per-commit body: base(48) + K=4 onion entries + K=4 witnesses
		// (145 B each = 580 B). Onion: L_0 plaintext (~111 B) + 3×IBE-wrapped
		// layers (~159 B each) ≈ 588 B. Total body ~1216 B; cluster ~14.6 KB.
		// Observed sim ~12.5 KB (some commits omit own-leader-layer onion
		// entries; see phase2.go own-leader skip).
		{"Commit", 10000, 15000},
		// Certificate: N=4 ops × (N-1)=3 recipients × ~151 B cert body
		// ≈ 1812 B. Stub cert = ClusterID(32) + Height(8) + V(~15) + Sig(96).
		{"Certificate", 1400, 2400},
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
// from real SignedSSVMessage encoding. PROPOSE + PREPARE + COMMIT produce
// distinct per-kind buckets at canonical config.
func TestBandwidth_Healthy_QBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)

	require.Greater(t, out.Bandwidth.TotalBytes, int64(0),
		"healthy QBFT must record non-zero bandwidth")

	// PROPOSE = LeaderBroadcast; PREPARE+COMMIT = Commit kind (mesh-shared).
	require.Greater(t, out.Bandwidth.PerKindBytes["LeaderBroadcast"], int64(0))
	require.Greater(t, out.Bandwidth.PerKindBytes["Commit"], int64(0))

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

	out, err := qbftadapter.Protocol{}.Run(cfg)
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

	out, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	// Honest leader at R2 (op2) emits one PROPOSE via virtualNetwork.Broadcast;
	// byz leader at R1 (op1) fabricates 3 distinct PROPOSEs (one per honest)
	// via evtByzProposal. Op1 should appear in PerOperatorOut even though it
	// has no Instance — that's only true if byz PROPOSE bytes are charged.
	require.Greater(t, out.Bandwidth.PerOperatorOut[1], int64(0),
		"byz leader op1 should have non-zero out-bytes from R1 PROPOSE fabrication: %s",
		out.Bandwidth.SummaryLine())
}
