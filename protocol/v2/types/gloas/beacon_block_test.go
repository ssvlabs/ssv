package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	bitfield "github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
)

// A Gloas block round-trips through SSZ with the ePBS-specific body fields populated (the payload bid,
// an aggregated payload attestation, and the parent execution requests), and its root is stable.
func TestSignedBeaconBlockRoundTrip(t *testing.T) {
	in := &SignedBeaconBlock{Message: &BeaconBlock{
		Slot:          7,
		ProposerIndex: 3,
		Body: &BeaconBlockBody{
			ETH1Data:      &phase0.ETH1Data{BlockHash: make([]byte, 32)},
			SyncAggregate: &altair.SyncAggregate{SyncCommitteeBits: bitfield.NewBitvector512()},
			SignedExecutionPayloadBid: &SignedExecutionPayloadBid{Message: &ExecutionPayloadBid{
				BuilderIndex:       BuilderIndexSelfBuild,
				BlobKZGCommitments: []deneb.KZGCommitment{{0x01}},
			}},
			PayloadAttestations: []*PayloadAttestation{{
				AggregationBits: bitfield.NewBitvector512(),
				Data:            &PayloadAttestationData{Slot: 6, PayloadPresent: true},
			}},
			ParentExecutionRequests: &ExecutionRequests{},
		},
	}}
	b, err := in.MarshalSSZ()
	require.NoError(t, err)

	out := &SignedBeaconBlock{}
	require.NoError(t, out.UnmarshalSSZ(b))
	require.Equal(t, in.Message.Slot, out.Message.Slot)
	require.Equal(t, BuilderIndexSelfBuild, out.Message.Body.SignedExecutionPayloadBid.Message.BuilderIndex)
	require.True(t, out.Message.Body.PayloadAttestations[0].Data.PayloadPresent)

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}
