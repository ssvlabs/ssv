package blind

import (
	"bytes"
	"testing"

	bitfield "github.com/OffchainLabs/go-bitfield"
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
	"github.com/stretchr/testify/require"
)

// TestEnsureBlinded_FuluProducesElectraEquivalentBytes asserts that EnsureBlinded's
// Fulu branch produces SSZ bytes byte-identical to its Electra branch given the same
// block body. Both branches build an apiv1electra.BlindedBeaconBlock (Fulu reuses the
// Electra blinded structures), but they are separate, duplicated field-copy blocks — so
// this guards against the two drifting: a field added, dropped, or reordered in one branch
// but not the other changes the bytes and fails here.
//
// Why the reuse is valid: for blinded blocks Lighthouse's Fulu and Electra bodies share a
// byte-identical SSZ schema (Fulu differs only in the unblinded ExecutionPayload, which the
// blinded form replaces with an identical ExecutionPayloadHeader). This test does not
// re-verify that Lighthouse-level claim — it only pins SSV's two branches to each other.
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
		"Fulu and Electra blinded SSZ differ (Fulu: %d bytes, Electra: %d bytes); "+
			"blind.go's two branches have drifted — reconcile them before releasing.",
		len(fuluBytes), len(electraBytes))

	// The bytes also decode back through apiv1electra.BlindedBeaconBlock — the schema
	// Lighthouse uses for both Electra and Fulu blinded bodies.
	var roundTrip apiv1electra.BlindedBeaconBlock
	require.NoError(t, roundTrip.UnmarshalSSZ(fuluBytes),
		"SSV's Fulu blinded output must round-trip through apiv1electra.BlindedBeaconBlock")
}
