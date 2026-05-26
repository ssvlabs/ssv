package twoab_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestCrash_L0LeaderDirect crashes the L_0 leader (op1). The three surviving
// honest ops fall through to a backup layer and reach σ-quorum; the crashed
// leader is reported offline and never counts as a decider.
func TestCrash_L0LeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest of 4 should still decide with the L_0 leader crashed")
	require.GreaterOrEqual(t, out.DecidedRound, 1, "L_0 leader crashed → decision falls through to a backup layer")
	require.False(t, out.PerOp[1].Decided, "crashed op must not be a decider")
	require.Equal(t, "offline", out.PerOp[1].Err)
}

// TestCrash_NonLeaderDirect crashes a non-leader (op4); the healthy L_0 path
// holds for the three remaining honest ops.
func TestCrash_NonLeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{4}}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "L_0 leader up + 3 honest signers → decide at L_0")
	require.Equal(t, 0, out.DecidedRound, "non-leader crash leaves the healthy L_0 path intact")
	require.Equal(t, "offline", out.PerOp[4].Err)
}

// TestCrash_Mesh crashes the L_0 leader under the production-realistic mesh;
// the mesh is rebuilt on the survivors with no connectivity panic.
func TestCrash_Mesh(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.SafetyBuffer = 700 * time.Millisecond
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := twoabadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy mesh on 3 survivors should still decide")
	require.Equal(t, "offline", out.PerOp[1].Err)
}

// TestCrash_CountRandomSelection drives the Healthy-knob path: CrashedCount=1
// resolves to exactly one offline op and the cluster still decides.
func TestCrash_CountRandomSelection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{CrashedCount: 1}
	out, err := twoabadapter.Protocol{}.Run(cfg)
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
