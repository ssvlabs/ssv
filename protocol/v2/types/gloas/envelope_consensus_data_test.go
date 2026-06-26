package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// EnvelopeConsensusData must stay wire-identical to spectypes.ProposerConsensusData (the same
// {Duty, Version, DataSSZ} shape) so the §6 QBFT value byte-matches what other clients encode in a
// mixed cluster. This is what justifies a distinct node-side type instead of reusing the spec one.
func TestEnvelopeConsensusDataWireMatchesProposerConsensusData(t *testing.T) {
	duty := spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleEnvelopeBuilder,
		PubKey:         phase0.BLSPubKey{0x01},
		Slot:           7,
		ValidatorIndex: 3,
	}
	dataSSZ := []byte{0x0a, 0x0b, 0x0c}

	env := &EnvelopeConsensusData{Duty: duty, Version: spec.DataVersionFulu, DataSSZ: dataSSZ}
	prop := &spectypes.ProposerConsensusData{Duty: duty, Version: spec.DataVersionFulu, DataSSZ: dataSSZ}

	envBytes, err := env.Encode()
	require.NoError(t, err)
	propBytes, err := prop.Encode()
	require.NoError(t, err)
	require.Equal(t, propBytes, envBytes, "envelope consensus data must encode identically to proposer consensus data")

	// Round-trip at the wire level — SSZ decodes a nil slice back as empty, so compare bytes, not structs.
	out := &EnvelopeConsensusData{}
	require.NoError(t, out.Decode(envBytes))
	reEncoded, err := out.Encode()
	require.NoError(t, err)
	require.Equal(t, envBytes, reEncoded)
	require.Equal(t, dataSSZ, out.DataSSZ)
	require.Equal(t, duty.ValidatorIndex, out.Duty.ValidatorIndex)
}
