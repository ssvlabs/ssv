package qbft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// TestRun_PostConsensusQuorumMiss — a regression test: at n=4 with
// f+1=2 slow receivers (op2, op3 at 3·BTT inbound delay), consensus on the
// fast subset (op1, op4) completes but the cluster can't accumulate 2f+1
// post-consensus partial sigs in time because the slow ops never decide
// consensus and emit their own partials. The outcome must report:
//
//   - out.Decided = false (cluster missed the submit boundary)
//   - per-op Err = "no postconsensus quorum" for the fast ops that
//     consensus-decided locally (op1, op4)
//   - per-op Err = "did not decide before sim end" for the slow ops (op2, op3)
//
// Pins the semantic ("DecisionTime = earliest 2f+1 partial sigs",
// not "any op consensus-decided + fixed PhaseBudget").
//
// The fast/slow split needs a round timeout between the fast ops'
// consensus-completion (~5·BTT) and the slow ops' (~7·BTT) — neither the
// pristine floor (R1 = 3·BTT, too tight) nor QBFTSSV (flat 2s, wide enough
// that the slow ops also decide) expresses it. So this internal test drives
// the shared run() helper with a flat 6·BTT timeout directly. The
// quorum-accounting under test is RT-independent; the explicit RT only stages
// the split deterministically.
func TestRun_PostConsensusQuorumMiss(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Network = ct.PerReceiverDelay{
		Inner: ct.ConstantDelay{D: 200 * time.Millisecond},
		Overrides: map[ct.OperatorID]time.Duration{
			ct.OperatorID(2): 600 * time.Millisecond,
			ct.OperatorID(3): 600 * time.Millisecond,
		},
	}
	out, err := run(cfg, 1200*time.Millisecond, 0)
	require.NoError(t, err)
	require.False(t, out.Decided, "cluster should miss without 2f+1 partial sigs")
	require.Equal(t, DiagNoPostConsensusQuorum, out.PerOp[1].Err, "op1 consensus-decided locally but lacked partial-sig quorum")
	require.Equal(t, DiagNoPostConsensusQuorum, out.PerOp[4].Err, "op4 consensus-decided locally but lacked partial-sig quorum")
	require.Equal(t, DiagDidNotDecideBeforeSimEnd, out.PerOp[2].Err, "op2 (slow) didn't reach consensus")
	require.Equal(t, DiagDidNotDecideBeforeSimEnd, out.PerOp[3].Err, "op3 (slow) didn't reach consensus")
	require.Equal(t, "Cluster agreed on a value, but never gathered enough post-consensus partial signatures", out.MissReason,
		"MissReason should distinguish post-consensus quorum miss from rounds-exhausted miss")
}
