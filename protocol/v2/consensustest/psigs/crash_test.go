package psigs_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	psigsadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/psigs"
)

// TestCrash_Direct crashes one operator. PSigs has no leader, so any single
// crash just removes one of the qV=3 partial-sig sources; the remaining 3
// honest ops at n=4 still reach the threshold. The crashed op is reported
// offline and never counts as a decider.
func TestCrash_Direct(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{1}}
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "3 honest signers meet qV=3 with one op down")
	require.False(t, out.PerOp[1].Decided, "crashed op must not be a decider")
	require.Equal(t, "offline", out.PerOp[1].Err)
}

// TestCrash_Mesh crashes one operator under the production-realistic mesh;
// the mesh is rebuilt on the 3 survivors with no connectivity panic.
func TestCrash_Mesh(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.Byz = ct.ByzPattern{Crashed: []ct.OperatorID{4}}
	out, err := psigsadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy mesh on 3 survivors should still collect qV partials")
	require.Equal(t, "offline", out.PerOp[4].Err)
}

// TestCrash_CountRandomSelection drives the Healthy-knob path: CrashedCount=1
// resolves to exactly one offline op and the cluster still decides.
func TestCrash_CountRandomSelection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{CrashedCount: 1}
	out, err := psigsadapter.Protocol{}.Run(cfg)
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
