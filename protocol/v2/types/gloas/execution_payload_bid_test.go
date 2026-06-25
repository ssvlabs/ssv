package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/stretchr/testify/require"
)

func TestExecutionPayloadBidRoundTrip(t *testing.T) {
	in := &SignedExecutionPayloadBid{Message: &ExecutionPayloadBid{
		BlockHash:          [32]byte{0xaa},
		BuilderIndex:       BuilderIndexSelfBuild,
		Value:              123,
		BlobKZGCommitments: []deneb.KZGCommitment{{0x01}, {0x02}},
	}}
	b, err := in.MarshalSSZ()
	require.NoError(t, err)

	out := &SignedExecutionPayloadBid{}
	require.NoError(t, out.UnmarshalSSZ(b))
	require.Equal(t, BuilderIndexSelfBuild, out.Message.BuilderIndex)
	require.Len(t, out.Message.BlobKZGCommitments, 2)

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}
