package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestProposedBlocks(t *testing.T) {
	s := NewProposedBlocks()

	_, ok := s.Get(5)
	require.False(t, ok)

	decision := ProposedBlock{BlockRoot: phase0.Root{0x01}, ParentRoot: phase0.Root{0x02}, ExecutionRequestsRoot: phase0.Root{0x03}, ProducedLocally: true}
	s.Record(5, decision)
	got, ok := s.Get(5)
	require.True(t, ok)
	require.Equal(t, decision, got)

	// A far-future slot evicts decisions beyond the retention window.
	s.Record(20, ProposedBlock{BlockRoot: phase0.Root{0x04}})
	_, ok = s.Get(5)
	require.False(t, ok, "slot 5 should be evicted beyond the retention window")
	got, ok = s.Get(20)
	require.True(t, ok)
	require.Equal(t, phase0.Root{0x04}, got.BlockRoot)
}

// Binds is the §6 binding check (SIP #94 §6): the envelope must be self-build and commit to the decided
// block's root, its parent root, and the requests root the bid commits to; PayloadRoot is not checked.
func TestProposedBlockBinds(t *testing.T) {
	requests := &specgloas.ExecutionRequests{}
	requestsRoot, err := requests.HashTreeRoot()
	require.NoError(t, err)

	decision := ProposedBlock{BlockRoot: phase0.Root{0xaa}, ParentRoot: phase0.Root{0xbb}, ExecutionRequestsRoot: phase0.Root(requestsRoot)}
	binding := func() *gloas.BlindedExecutionPayloadEnvelope {
		return &gloas.BlindedExecutionPayloadEnvelope{
			PayloadRoot:           phase0.Root{0x09},
			ExecutionRequests:     requests,
			BuilderIndex:          specgloas.BuilderIndexSelfBuild,
			BeaconBlockRoot:       phase0.Root{0xaa},
			ParentBeaconBlockRoot: phase0.Root{0xbb},
		}
	}

	require.True(t, decision.Binds(binding()))

	// PayloadRoot is trusted from the builder operator: a different one still binds.
	other := binding()
	other.PayloadRoot = phase0.Root{0x10}
	require.True(t, decision.Binds(other))

	notSelfBuild := binding()
	notSelfBuild.BuilderIndex = 5
	require.False(t, decision.Binds(notSelfBuild))

	wrongBlock := binding()
	wrongBlock.BeaconBlockRoot = phase0.Root{0xcc}
	require.False(t, decision.Binds(wrongBlock))

	wrongParent := binding()
	wrongParent.ParentBeaconBlockRoot = phase0.Root{0xcc}
	require.False(t, decision.Binds(wrongParent))

	wrongRequests := binding()
	wrongRequests.ExecutionRequests = &specgloas.ExecutionRequests{BuilderExits: []*specgloas.BuilderExitRequest{{}}}
	require.False(t, decision.Binds(wrongRequests))

	require.False(t, decision.Binds(nil))
	require.False(t, decision.Binds(&gloas.BlindedExecutionPayloadEnvelope{}))
}
