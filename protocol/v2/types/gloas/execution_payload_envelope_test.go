package gloas

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"
)

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
		BaseFeePerGas:   uint256.NewInt(0x66),
		BlockHash:       phase0.Hash32{0x77},
		Transactions:    []bellatrix.Transaction{{0x01, 0x02}},
		Withdrawals:     []*capella.Withdrawal{{Index: 1, ValidatorIndex: 2, Address: bellatrix.ExecutionAddress{0x88}, Amount: 99}},
		BlobGasUsed:     1,
		ExcessBlobGas:   2,
		BlockAccessList: []byte{0xaa, 0xbb, 0xcc},
		SlotNumber:      7,
	}
}

// The blinding property §6 relies on: the full envelope's root equals the blinded envelope's when
// PayloadRoot = hash_tree_root(Payload), so a signature over the blinded root is valid for the full one.
// The blinded form is ssv-spec's wire type, so this also pins that re-typing the request lists into it
// preserves their root.
func TestExecutionPayloadEnvelopeBlindsToSameRoot(t *testing.T) {
	full := &ExecutionPayloadEnvelope{
		Payload: sampleExecutionPayload(),
		ExecutionRequests: &ExecutionRequests{
			BuilderDeposits: []*BuilderDepositRequest{{
				Pubkey:                phase0.BLSPubKey{0x01},
				WithdrawalCredentials: make([]byte, 32),
				Amount:                32_000_000_000,
				Signature:             phase0.BLSSignature{0x02},
			}},
			BuilderExits: []*BuilderExitRequest{{SourceAddress: bellatrix.ExecutionAddress{0x03}, Pubkey: phase0.BLSPubKey{0x04}}},
		},
		BuilderIndex:          BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0x02},
		ParentBeaconBlockRoot: phase0.Root{0x03},
	}

	blinded, err := Blinded(full)
	require.NoError(t, err)
	require.Equal(t, specgloas.BuilderIndexSelfBuild, blinded.BuilderIndex)
	require.Equal(t, full.BeaconBlockRoot, blinded.BeaconBlockRoot)
	require.Equal(t, full.ParentBeaconBlockRoot, blinded.ParentBeaconBlockRoot)

	payloadRoot, err := full.Payload.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, phase0.Root(payloadRoot), blinded.PayloadRoot)

	fullRequestsRoot, err := full.ExecutionRequests.HashTreeRoot()
	require.NoError(t, err)
	blindedRequestsRoot, err := blinded.ExecutionRequests.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, fullRequestsRoot, blindedRequestsRoot, "re-typed request lists must keep their root")

	fullRoot, err := full.HashTreeRoot()
	require.NoError(t, err)
	blindedRoot, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, blindedRoot, fullRoot, "blinded envelope must hash to the same root as the full envelope")
}

// hash256FromLowU64BE mirrors Lighthouse's Hash256::from_low_u64_be: the value in the last 8 bytes,
// big-endian.
func hash256FromLowU64BE(v uint64) phase0.Root {
	var r phase0.Root
	for i := 0; i < 8; i++ {
		r[31-i] = byte(v >> (8 * i))
	}
	return r
}

// SIP #94 §6 requires blinded-to-full root equivalence to be tested against a fixed expected root, not
// only by comparing two locally derived values: a same-implementation comparison passes even when both
// sides share the same incorrect merkleization. This is Anchor's fixture (a default payload and empty
// requests, builder index 42, block root 0x1111, parent root 0x2222) and its pinned root, which Anchor
// cross-checked against the consensus-specs pyspec at the SIP's pin, so the vector is a second- and
// third-implementation check of the progressive-container merkleization.
func TestBlindedExecutionPayloadEnvelopeFixedRootMatchesAnchor(t *testing.T) {
	expected, err := hex.DecodeString("9af9a50572381e869605147c3d6220c969d6f12087d94393ec0440660f752c5e")
	require.NoError(t, err)

	full := &ExecutionPayloadEnvelope{
		Payload: &ExecutionPayload{
			BaseFeePerGas: uint256.NewInt(0),
		},
		ExecutionRequests:     &ExecutionRequests{},
		BuilderIndex:          42,
		BeaconBlockRoot:       hash256FromLowU64BE(0x1111),
		ParentBeaconBlockRoot: hash256FromLowU64BE(0x2222),
	}

	blinded, err := Blinded(full)
	require.NoError(t, err)

	blindedRoot, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(expected), hex.EncodeToString(blindedRoot[:]), "blinded envelope root must match the cross-implementation vector")

	fullRoot, err := full.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, blindedRoot, fullRoot)
}

// The wire type survives an SSZ round trip with a stable root.
func TestBlindedExecutionPayloadEnvelopeRoundTrip(t *testing.T) {
	in := &BlindedExecutionPayloadEnvelope{
		PayloadRoot:           phase0.Root{0x01},
		ExecutionRequests:     &specgloas.ExecutionRequests{},
		BuilderIndex:          specgloas.BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0x02},
		ParentBeaconBlockRoot: phase0.Root{0x03},
	}
	b, err := in.Encode()
	require.NoError(t, err)

	out := &BlindedExecutionPayloadEnvelope{}
	require.NoError(t, out.Decode(b))
	require.Equal(t, in.PayloadRoot, out.PayloadRoot)
	require.Equal(t, specgloas.BuilderIndexSelfBuild, out.BuilderIndex)
	require.Equal(t, in.BeaconBlockRoot, out.BeaconBlockRoot)
	require.Equal(t, in.ParentBeaconBlockRoot, out.ParentBeaconBlockRoot)

	r1, err := in.HashTreeRoot()
	require.NoError(t, err)
	r2, err := out.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, r1, r2)
}
