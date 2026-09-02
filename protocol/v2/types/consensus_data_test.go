package types

import (
	"testing"

	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// A Gloas-versioned aggregate-and-proof decodes into go-eth2-client's Gloas container, whose hash tree
// root — the aggregator's signing root — differs from the byte-identical Electra container's (#3009).
func TestGetAggregateAndProofGloas(t *testing.T) {
	aggregationBits := bitfield.NewBitlist(8)
	aggregationBits.SetBitAt(2, true)
	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(1, true)
	in := &eth2gloas.AggregateAndProof{
		AggregatorIndex: 64,
		Aggregate: &eth2gloas.Attestation{
			AggregationBits: aggregationBits,
			Data: &phase0.AttestationData{
				Slot:            405,
				Index:           1,
				BeaconBlockRoot: phase0.Root{0x01},
				Source:          &phase0.Checkpoint{Epoch: 11, Root: phase0.Root{0x02}},
				Target:          &phase0.Checkpoint{Epoch: 12, Root: phase0.Root{0x03}},
			},
			Signature:     phase0.BLSSignature{0xaa},
			CommitteeBits: committeeBits,
		},
		SelectionProof: phase0.BLSSignature{0xbb},
	}
	dataSSZ, err := in.MarshalSSZ()
	require.NoError(t, err)

	versioned, hashRoot, err := GetAggregateAndProof(&spectypes.ProposerConsensusData{Version: spec.DataVersionGloas, DataSSZ: dataSSZ})
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionGloas, versioned.Version)
	require.NotNil(t, versioned.Gloas)
	require.Nil(t, versioned.Electra)
	require.Nil(t, versioned.Fulu)
	require.Same(t, versioned.Gloas, hashRoot)
	require.Equal(t, in.AggregatorIndex, versioned.Gloas.AggregatorIndex)
	require.Equal(t, in.SelectionProof, versioned.Gloas.SelectionProof)
	require.Equal(t, in.Aggregate.Data, versioned.Gloas.Aggregate.Data)

	wantRoot, err := in.HashTreeRoot()
	require.NoError(t, err)
	gotRoot, err := hashRoot.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, wantRoot, gotRoot)

	// The same bytes under the Fulu arm land in the Electra container, whose root differs — the reason
	// Gloas cannot reuse the Electra decode path.
	fuluVersioned, fuluHashRoot, err := GetAggregateAndProof(&spectypes.ProposerConsensusData{Version: spec.DataVersionFulu, DataSSZ: dataSSZ})
	require.NoError(t, err)
	require.IsType(t, &electra.AggregateAndProof{}, fuluVersioned.Fulu)
	electraRoot, err := fuluHashRoot.HashTreeRoot()
	require.NoError(t, err)
	require.NotEqual(t, gotRoot, electraRoot)
}

func TestGetAggregateAndProofUnknownVersion(t *testing.T) {
	_, _, err := GetAggregateAndProof(&spectypes.ProposerConsensusData{Version: spec.DataVersion(99)})
	require.ErrorContains(t, err, "unknown aggregate and proof version")
}
