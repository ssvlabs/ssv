package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
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

func sampleExecutionPayload() *ExecutionPayload {
	return &ExecutionPayload{
		ParentHash:      phase0.Hash32{0x11},
		FeeRecipient:    bellatrix.ExecutionAddress{0x22},
		StateRoot:       phase0.Root{0x33},
		ReceiptsRoot:    phase0.Root{0x44},
		PrevRandao:      phase0.Hash32{0x55},
		BlockNumber:     42,
		GasLimit:        30_000_000,
		GasUsed:         21_000,
		Timestamp:       1_700_000_000,
		ExtraData:       []byte("ssv"),
		BaseFeePerGas:   [32]byte{0x66},
		BlockHash:       phase0.Hash32{0x77},
		Transactions:    []bellatrix.Transaction{{0x01, 0x02}},
		Withdrawals:     []*capella.Withdrawal{{Index: 1, ValidatorIndex: 2, Address: bellatrix.ExecutionAddress{0x88}, Amount: 99}},
		BlobGasUsed:     1,
		ExcessBlobGas:   2,
		BlockAccessList: []byte{0xaa, 0xbb, 0xcc},
		SlotNumber:      7,
	}
}

// The full Gloas ExecutionPayload round-trips through SSZ with a stable root.
func TestExecutionPayloadRoundTrip(t *testing.T) {
	in := sampleExecutionPayload()
	b, err := in.MarshalSSZ()
	require.NoError(t, err)

	out := &ExecutionPayload{}
	require.NoError(t, out.UnmarshalSSZ(b))

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}

// The full envelope round-trips through SSZ with a stable root.
func TestExecutionPayloadEnvelopeRoundTrip(t *testing.T) {
	in := &ExecutionPayloadEnvelope{
		Payload:               sampleExecutionPayload(),
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0x02},
		ParentBeaconBlockRoot: phase0.Root{0x03},
	}
	b, err := in.MarshalSSZ()
	require.NoError(t, err)

	out := &ExecutionPayloadEnvelope{}
	require.NoError(t, out.UnmarshalSSZ(b))

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}

// The blinding property §6 relies on: the full envelope's root equals the blinded envelope's when
// PayloadRoot = hash_tree_root(Payload), so a signature over the blinded root is valid for the full one.
func TestExecutionPayloadEnvelopeBlindsToSameRoot(t *testing.T) {
	full := &ExecutionPayloadEnvelope{
		Payload:               sampleExecutionPayload(),
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0x02},
		ParentBeaconBlockRoot: phase0.Root{0x03},
	}

	blinded, err := full.Blinded()
	require.NoError(t, err)

	fullRoot, err := full.HashTreeRoot()
	require.NoError(t, err)
	blindedRoot, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, blindedRoot, fullRoot, "blinded envelope must hash to the same root as the full envelope")
}
