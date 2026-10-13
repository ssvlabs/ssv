package types

import (
	"crypto/sha256"
	"encoding/hex"
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

// TestGetAggregateAndProofGloasFixedVector pins the Gloas aggregate's roots to values from an independent
// implementation: the self-consistency checks above pass even when both sides share a wrong merkleization,
// the "mixed merkleization" hazard SIP #94 §2 calls consensus-critical. The values come from the
// consensus-specs pyspec at the commit SIP #94 pins; testdata/gloas_aggregate_and_proof_fixture.py rebuilds
// the fixture there and records that run's output. Its 700-bit aggregation bitlist spans three chunks, so
// the progressive bitlist padding is exercised as well as the progressive container.
func TestGetAggregateAndProofGloasFixedVector(t *testing.T) {
	const (
		wantSSZSHA256       = "a3010361b4058e8668275075d425c0bee5cb704851eaf94b0dceb411802929bb"
		wantAttestationRoot = "c67fcdd0fc5173cea66fa19b4b2fc26c6c5de463c9d7358fdad7952341931372"
		wantRoot            = "25d7a728d9874ba5baf1087c0cefcbf97fa9daa39d8e62fbd7a699042f068c06"
		wantDomain          = "06000000d620b8f54e1c0237c64157679dd01e643a0911ba1344e568ee73e279"
		wantSigningRoot     = "3934b9a7a7c92a7329790a8fe2ed98d15a9884a6425bc58b406c9450478429a0"
		genesisValsRoot     = "bb4a1a9e3f7f4e10edcd734e4acc3b5ffd4f830efe0af2748fa458cfee5d2658"
	)
	forkVersion := phase0.Version{0x80, 0x73, 0x31, 0x83}

	aggregationBits := bitfield.NewBitlist(700)
	for _, i := range []uint64{0, 5, 255, 256, 511, 699} {
		aggregationBits.SetBitAt(i, true)
	}
	committeeBits := bitfield.NewBitvector64()
	for _, i := range []uint64{1, 7, 63} {
		committeeBits.SetBitAt(i, true)
	}
	var signature, selectionProof phase0.BLSSignature
	for i := range signature {
		signature[i] = byte(i)
		selectionProof[i] = byte(0xbb ^ i)
	}
	filledRoot := func(b byte) (r phase0.Root) {
		for i := range r {
			r[i] = b
		}
		return r
	}
	in := &eth2gloas.AggregateAndProof{
		AggregatorIndex: 4242,
		Aggregate: &eth2gloas.Attestation{
			AggregationBits: aggregationBits,
			Data: &phase0.AttestationData{
				Slot:            1234567,
				Index:           1,
				BeaconBlockRoot: filledRoot(0x01),
				Source:          &phase0.Checkpoint{Epoch: 38579, Root: filledRoot(0x02)},
				Target:          &phase0.Checkpoint{Epoch: 38580, Root: filledRoot(0x03)},
			},
			Signature:     signature,
			CommitteeBits: committeeBits,
		},
		SelectionProof: selectionProof,
	}

	// The serialization hash pins the fixture itself: a mismatch here means a different object, not a
	// different merkleization.
	dataSSZ, err := in.MarshalSSZ()
	require.NoError(t, err)
	sszSHA256 := sha256.Sum256(dataSSZ)
	require.Equal(t, wantSSZSHA256, hex.EncodeToString(sszSHA256[:]))

	_, hashRoot, err := GetAggregateAndProof(&spectypes.ProposerConsensusData{Version: spec.DataVersionGloas, DataSSZ: dataSSZ})
	require.NoError(t, err)
	aggregateAndProof, ok := hashRoot.(*eth2gloas.AggregateAndProof)
	require.True(t, ok)

	attestationRoot, err := aggregateAndProof.Aggregate.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, wantAttestationRoot, hex.EncodeToString(attestationRoot[:]), "progressive Attestation root must match the pyspec")

	root, err := hashRoot.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, wantRoot, hex.EncodeToString(root[:]), "AggregateAndProof root must match the pyspec")

	genesisValidatorsRoot, err := hex.DecodeString(genesisValsRoot)
	require.NoError(t, err)
	domain, err := spectypes.ComputeETHDomain(spectypes.DomainAggregateAndProof, forkVersion, phase0.Root(genesisValidatorsRoot))
	require.NoError(t, err)
	require.Equal(t, wantDomain, hex.EncodeToString(domain[:]))

	signingRoot, err := spectypes.ComputeETHSigningRoot(hashRoot, domain)
	require.NoError(t, err)
	require.Equal(t, wantSigningRoot, hex.EncodeToString(signingRoot[:]), "aggregator signing root must match the pyspec")
}

func TestGetAggregateAndProofUnknownVersion(t *testing.T) {
	_, _, err := GetAggregateAndProof(&spectypes.ProposerConsensusData{Version: spec.DataVersion(99)})
	require.ErrorContains(t, err, "unknown aggregate and proof version")
}
