package obft_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
)

// TestCrash_L0LeaderDirect crashes the L_0 leader (op1) entirely. The three
// surviving honest ops fall through to L_1 and reach σ-quorum (qV=3 at n=4),
// while the crashed leader is reported offline and never counts as a decider.
func TestCrash_L0LeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest of 4 should still decide with the L_0 leader crashed")
	require.GreaterOrEqual(t, out.DecidedRound, 1, "L_0 leader crashed → decision falls through to a backup layer")

	op1 := out.PerOp[1]
	require.False(t, op1.Decided, "crashed op must not be a decider")
	require.Equal(t, "offline", op1.Err)
}

// TestCrash_NonLeaderDirect crashes a non-leader (op4). The L_0 leader still
// broadcasts and the three remaining honest ops reach σ-quorum at L_0.
func TestCrash_NonLeaderDirect(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "L_0 leader up + 3 honest signers → decide at L_0")
	require.Equal(t, 0, out.DecidedRound, "non-leader crash leaves the healthy L_0 path intact")
	require.Equal(t, "offline", out.PerOp[4].Err)
}

// TestCrash_CountRandomSelection drives the Healthy knob path: CrashedCount=1
// with no explicit set, so Validate draws one victim from the seed. The cell
// still decides (≤ f crashes) and exactly one op is reported offline.
func TestCrash_CountRandomSelection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{CrashedCount: 1}
	out, err := obftadapter.Protocol{}.Run(cfg)
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

// TestCrash_Mesh crashes the L_0 leader under the production-realistic mesh
// transport. The mesh is rebuilt on the 3 surviving cluster peers (no
// connectivity panic) and the cluster still decides.
func TestCrash_Mesh(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.SafetyBuffer = 700 * time.Millisecond
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy mesh on 3 survivors should still decide")
	require.Equal(t, "offline", out.PerOp[1].Err)
}

// TestCrash_PlusByzExceedsBudget pins the f-budget: at n=4 (f=1) one crash
// plus one byzantine op is 2 > f, which Validate rejects (surfaced as
// ErrConfigOutOfEnvelope by the adapter).
func TestCrash_PlusByzExceedsBudget(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzSilentLeader,
		ByzOperators: []ct.OperatorID{2},
		Crashed:      []ct.OperatorID{3},
	}
	_, err := obftadapter.Protocol{}.Run(cfg)
	require.ErrorIs(t, err, ct.ErrConfigOutOfEnvelope)
}
