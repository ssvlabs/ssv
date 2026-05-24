package qbft_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestCrash_R1LeaderDirect crashes the round-1 leader (op1). R1 times out,
// the cluster round-changes to an honest leader and decides; the crashed
// leader is reported offline and never counts as a decider.
func TestCrash_R1LeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest of 4 should still decide after round-changing past the crashed R1 leader")
	require.GreaterOrEqual(t, out.DecidedRound, 1, "R1 leader crashed → decision at a later round (DecidedRound is 0-indexed: R2 → 1)")
	require.False(t, out.PerOp[1].Decided, "crashed op must not be a decider")
	require.Equal(t, "offline", out.PerOp[1].Err)
}

// TestCrash_NonLeaderDirect crashes a non-leader (op4); the three remaining
// honest ops reach consensus + post-consensus quorum (2f+1=3) at round 1.
func TestCrash_NonLeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{4}}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest signers meet quorum at R1 with one non-leader down")
	require.Equal(t, "offline", out.PerOp[4].Err)
}

// TestCrash_Mesh crashes a non-leader under the production-realistic mesh:
// the mesh is rebuilt on the 3 surviving cluster peers (no connectivity
// panic) and the cluster still decides at R1 without a round-change. (A
// crashed R1 *leader* would force a round-change whose RT timeout can miss
// the 4s deadline under the slower mesh transport — a real protocol cost,
// not a crash-wiring bug — so this test isolates the mesh+crash path from
// the round-change timing confound.)
func TestCrash_Mesh(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{4}}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy mesh on 3 survivors should decide at R1 with one non-leader down")
	require.Equal(t, "offline", out.PerOp[4].Err)
}

// TestCrash_CountRandomSelection drives the Healthy-knob path: CrashedCount=1
// resolves to exactly one offline op and the cluster still decides.
func TestCrash_CountRandomSelection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{CrashedCount: 1}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "a single crash is within the f=1 budget at n=4")
	offline := 0
	for _, oo := range out.PerOp {
		if oo.Err == "offline" {
			offline++
		}
	}
	require.Equal(t, 1, offline, "CrashedCount=1 must resolve to exactly one offline op")
}
