package goclient

import (
	"testing"

	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func gloasTestAttestation() *eth2gloas.Attestation {
	aggregationBits := bitfield.NewBitlist(8)
	aggregationBits.SetBitAt(2, true)
	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(1, true)
	return &eth2gloas.Attestation{
		AggregationBits: aggregationBits,
		Data: &phase0.AttestationData{
			Slot:            405,
			Index:           1, // the Gloas payload-status index (FULL), kept as the BN supplied it
			BeaconBlockRoot: phase0.Root{0x01},
			Source:          &phase0.Checkpoint{Epoch: 11, Root: phase0.Root{0x02}},
			Target:          &phase0.Checkpoint{Epoch: 12, Root: phase0.Root{0x03}},
		},
		Signature:     phase0.BLSSignature{0xaa},
		CommitteeBits: committeeBits,
	}
}

// Gloas aggregates ride go-eth2-client's dedicated Gloas container (#3009): it serializes exactly like
// the Electra one but merkleizes progressively, so the aggregate-and-proof root the aggregator signs
// differs from what the Electra container would give for the same bytes.
func TestVersionedAggregateGloas(t *testing.T) {
	t.Parallel()

	att := gloasTestAttestation()
	va := &spec.VersionedAttestation{Version: spec.DataVersionGloas, Gloas: att}

	obj, version, err := versionedAggregateToSSZ(va)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionGloas, version)
	require.Same(t, att, obj)

	selectionProof := phase0.BLSSignature{0xbb}
	obj, version, err = versionedToAggregateAndProof(va, 64, selectionProof)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionGloas, version)
	aggregateAndProof, ok := obj.(*eth2gloas.AggregateAndProof)
	require.True(t, ok, "got %T", obj)
	require.EqualValues(t, 64, aggregateAndProof.AggregatorIndex)
	require.Same(t, att, aggregateAndProof.Aggregate)
	require.Equal(t, selectionProof, aggregateAndProof.SelectionProof)

	electraShaped := &electra.AggregateAndProof{
		AggregatorIndex: 64,
		Aggregate: &electra.Attestation{
			AggregationBits: att.AggregationBits,
			Data:            att.Data,
			Signature:       att.Signature,
			CommitteeBits:   att.CommitteeBits,
		},
		SelectionProof: selectionProof,
	}
	gloasBytes, err := aggregateAndProof.MarshalSSZ()
	require.NoError(t, err)
	electraBytes, err := electraShaped.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, electraBytes, gloasBytes, "serialization is identical across the two containers")

	gloasRoot, err := aggregateAndProof.HashTreeRoot()
	require.NoError(t, err)
	electraRoot, err := electraShaped.HashTreeRoot()
	require.NoError(t, err)
	require.NotEqual(t, electraRoot, gloasRoot, "the Gloas container must not hash like the Electra one")
}
