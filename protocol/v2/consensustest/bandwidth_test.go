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
// healthy slot. Asserts loose upper bounds (~ ≤ 30 KB total at n=4) so the
// test stays robust to message-format evolution while catching gross
// regressions.
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

	// Per-operator: every op both sends and receives in healthy round.
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
