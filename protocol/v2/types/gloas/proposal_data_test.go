package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"
)

// The node's wrapper is byte-identical on the wire to ssv-spec's GloasProposalData, in both directions,
// with the same root — the cluster decides on it, so every implementation must decode the same bytes.
func TestGloasProposalDataMatchesSpecWire(t *testing.T) {
	node := &GloasProposalData{Block: TestingBeaconBlock(8), PayloadRoot: phase0.Root{0x50, 0x51, 0x52}}
	nodeBytes, err := node.Encode()
	require.NoError(t, err)

	spec := &specgloas.GloasProposalData{}
	require.NoError(t, spec.UnmarshalSSZ(nodeBytes))
	require.Equal(t, node.PayloadRoot, spec.PayloadRoot)
	require.Equal(t, node.Block.Slot, spec.Block.Slot)
	specBytes, err := spec.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, nodeBytes, specBytes)

	nodeRoot, err := node.HashTreeRoot()
	require.NoError(t, err)
	specRoot, err := spec.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, specRoot, nodeRoot)

	decoded, err := DecodeGloasProposalData(specBytes)
	require.NoError(t, err)
	require.Equal(t, node.PayloadRoot, decoded.PayloadRoot)
	require.True(t, decoded.SelfBuild())
}

func TestDecodeGloasProposalDataRejectsGarbage(t *testing.T) {
	_, err := DecodeGloasProposalData([]byte("garbage"))
	require.Error(t, err)
}

func TestGloasProposalDataSelfBuild(t *testing.T) {
	require.True(t, (&GloasProposalData{Block: TestingBeaconBlock(8)}).SelfBuild())
	external := TestingBeaconBlock(8)
	external.Body.SignedExecutionPayloadBid.Message.BuilderIndex = 3
	require.False(t, (&GloasProposalData{Block: external}).SelfBuild())
}

// The envelope derived from the decided value alone is exactly the blinded form of the builder operator's
// full envelope — same fields, same root — so the threshold signature over the derived root is valid for
// the reveal (SIP #94 §6).
func TestDeriveBlindedEnvelopeMatchesBuiltEnvelope(t *testing.T) {
	block := TestingBeaconBlock(8)
	blockRoot, err := block.HashTreeRoot()
	require.NoError(t, err)
	full := &ExecutionPayloadEnvelope{
		Payload:               sampleExecutionPayload(),
		ExecutionRequests:     &ExecutionRequests{}, // the requests the block's bid commits to
		BuilderIndex:          BuilderIndexSelfBuild,
		BeaconBlockRoot:       blockRoot,
		ParentBeaconBlockRoot: block.ParentRoot,
	}
	payloadRoot, err := full.Payload.HashTreeRoot()
	require.NoError(t, err)

	derived, err := (&GloasProposalData{Block: block, PayloadRoot: payloadRoot}).DeriveBlindedEnvelope()
	require.NoError(t, err)
	blinded, err := Blinded(full)
	require.NoError(t, err)
	require.Equal(t, blinded, derived)

	derivedRoot, err := derived.HashTreeRoot()
	require.NoError(t, err)
	fullRoot, err := full.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, fullRoot, derivedRoot)
}
