package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// A blinded envelope round-trips through SSZ and its root is stable.
func TestBlindedExecutionPayloadEnvelopeRoundTrip(t *testing.T) {
	in := &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           phase0.Root{0x01},
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0x02},
		ParentBeaconBlockRoot: phase0.Root{0x03},
	}
	b, err := in.MarshalSSZ()
	require.NoError(t, err)

	out := &BlindedExecutionPayloadEnvelope{}
	require.NoError(t, out.UnmarshalSSZ(b))
	require.Equal(t, in.PayloadRoot, out.PayloadRoot)
	require.Equal(t, BuilderIndexSelfBuild, out.BuilderIndex)
	require.Equal(t, in.BeaconBlockRoot, out.BeaconBlockRoot)
	require.Equal(t, in.ParentBeaconBlockRoot, out.ParentBeaconBlockRoot)

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}
