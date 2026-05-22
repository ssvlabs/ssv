package blind

import (
	"bytes"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	denebspec "github.com/attestantio/go-eth2-client/spec/deneb"
	electraspec "github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	bitfield "github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
)

// TestEnsureBlinded_FuluProducesElectraEquivalentBytes verifies that SSV's
// deliberate reuse of apiv1electra.BlindedBeaconBlock for Fulu (documented in
// EnsureBlinded's DataVersionFulu branch: "Fulu reuses Electra blinded block
// structures in this codebase") produces SSZ bytes that are byte-identical to the
// equivalent Electra-versioned blinded block.
//
// Why this matters for cross-client interop:
//   - Lighthouse defines BeaconBlockBodyFulu and BeaconBlockBodyElectra as
//     distinct superstruct variants. They differ in exactly one declared field:
//     `execution_payload` (Payload::Fulu vs Payload::Electra).
//   - For BLINDED blocks specifically, the field becomes the
//     ExecutionPayloadHeader. Lighthouse's ExecutionPayloadHeaderFulu and
//     ExecutionPayloadHeaderElectra superstruct variants have **byte-identical**
//     SSZ schemas (verified at https://github.com/sigp/lighthouse/blob/e3ee7feb/
//     consensus/types/src/execution_payload_header.rs — same fields, same types,
//     same fork-gates).
//   - All 12 other BeaconBlockBody fields are identical between Electra and
//     Fulu, with identical superstruct gates that include both forks.
//   - Therefore, an SSZ-encoded BlindedBeaconBlockBodyElectra is, byte-for-byte,
//     a valid BlindedBeaconBlockBodyFulu for Lighthouse.
//
// This test pins SSV's behavior: any future Fulu-specific divergence in the
// blind.go encoder would break this test, surfacing the change before it can
// hit production interop.
func TestEnsureBlinded_FuluProducesElectraEquivalentBytes(t *testing.T) {
	// Build a deterministic ExecutionPayload, body, and block once; reuse for
	// both Electra and Fulu versions so the only difference is the version tag.
	makeBody := func() *electraspec.BeaconBlockBody {
		payload := &denebspec.ExecutionPayload{
			ParentHash:    phase0.Hash32{0x01},
			FeeRecipient:  bellatrix.ExecutionAddress{0x02},
			StateRoot:     phase0.Root{0x03},
			ReceiptsRoot:  phase0.Root{0x04},
			LogsBloom:     [256]byte{0x05},
			PrevRandao:    [32]byte{0x06},
			BlockNumber:   42,
			GasLimit:      30_000_000,
			GasUsed:       12_345_678,
			Timestamp:     1_700_000_000,
			ExtraData:     []byte{0x07, 0x08, 0x09},
			BaseFeePerGas: uint256.NewInt(10_000_000_000),
			BlockHash:     phase0.Hash32{0x0a},
			Transactions: []bellatrix.Transaction{
				bellatrix.Transaction([]byte("hello")),
				bellatrix.Transaction([]byte("world")),
			},
			Withdrawals: []*capella.Withdrawal{
				{Index: 100, ValidatorIndex: 200, Address: bellatrix.ExecutionAddress{0x0b}, Amount: 300_000_000},
			},
			BlobGasUsed:   131072,
			ExcessBlobGas: 262144,
		}

		sa := &altair.SyncAggregate{}
		sa.SyncCommitteeBits = bitfield.Bitvector512(make([]byte, 64))

		return &electraspec.BeaconBlockBody{
			RANDAOReveal:          phase0.BLSSignature{0x0c},
			ETH1Data:              &phase0.ETH1Data{DepositRoot: phase0.Root{0x0d}, DepositCount: 1, BlockHash: make([]byte, 32)},
			Graffiti:              [32]byte{0x0e},
			ProposerSlashings:     nil,
			AttesterSlashings:     nil,
			Attestations:          nil,
			Deposits:              nil,
			VoluntaryExits:        nil,
			SyncAggregate:         sa,
			ExecutionPayload:      payload,
			BLSToExecutionChanges: nil,
			BlobKZGCommitments:    nil,
			ExecutionRequests:     &electraspec.ExecutionRequests{},
		}
	}

	blk := &electraspec.BeaconBlock{
		Slot:          12345,
		ProposerIndex: 67890,
		ParentRoot:    phase0.Root{0x0f},
		StateRoot:     phase0.Root{0x10},
		Body:          makeBody(),
	}

	electraProposal := &api.VersionedProposal{
		Version: spec.DataVersionElectra,
		Electra: &apiv1electra.BlockContents{Block: blk},
	}
	fuluProposal := &api.VersionedProposal{
		Version: spec.DataVersionFulu,
		Fulu:    &apiv1fulu.BlockContents{Block: blk},
	}

	electraBlinded, electraMarshaler, err := EnsureBlinded(electraProposal)
	require.NoError(t, err)
	require.True(t, electraBlinded.Blinded)
	require.NotNil(t, electraBlinded.ElectraBlinded)

	fuluBlinded, fuluMarshaler, err := EnsureBlinded(fuluProposal)
	require.NoError(t, err)
	require.True(t, fuluBlinded.Blinded)
	require.NotNil(t, fuluBlinded.FuluBlinded)

	electraBytes, err := electraMarshaler.MarshalSSZ()
	require.NoError(t, err)
	fuluBytes, err := fuluMarshaler.MarshalSSZ()
	require.NoError(t, err)

	require.Truef(t, bytes.Equal(electraBytes, fuluBytes),
		"Electra and Fulu blinded SSZ bytes differ (Electra: %d bytes, Fulu: %d bytes). "+
			"SSV's blind.go reuses apiv1electra.BlindedBeaconBlock for Fulu, so they MUST produce "+
			"identical bytes given identical body content. If this fails, blind.go's Fulu branch "+
			"has diverged from the Electra branch in some byte-level way — investigate before any "+
			"release that touches blind.go.",
		len(electraBytes), len(fuluBytes))

	// Confirm round-trip: the bytes that SSV produces are decodable back through
	// apiv1electra.BlindedBeaconBlock — the schema Lighthouse uses for both
	// Electra and Fulu blinded bodies (verified via Lighthouse source inspection).
	var roundTrip apiv1electra.BlindedBeaconBlock
	require.NoError(t, roundTrip.UnmarshalSSZ(fuluBytes),
		"SSV's Fulu blinded output must round-trip through apiv1electra.BlindedBeaconBlock — "+
			"this is the same SSZ schema Lighthouse's BlindedBeaconBlockFulu uses.")

	t.Logf("Electra blinded SSZ: %d bytes", len(electraBytes))
	t.Logf("Fulu blinded SSZ:    %d bytes", len(fuluBytes))
	t.Logf("Bytes match: %v (round-trip OK)", bytes.Equal(electraBytes, fuluBytes))
}
