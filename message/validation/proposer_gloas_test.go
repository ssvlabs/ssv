package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// The Gloas proposer's post-consensus packet carries the block root and, on the self-build path, the §6
// blinded-envelope root (SIP #94 §7); every other validator-role packet, and the proposer's own pre-Gloas
// and pre-consensus packets, stay at one entry.
func TestMaxValidatorRoleSignatures(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}

	preGloasSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	require.Equal(t, 2, mv.maxValidatorRoleSignatures(spectypes.RoleProposer, spectypes.PostConsensusPartialSig, gloasSlot))
	require.Equal(t, 1, mv.maxValidatorRoleSignatures(spectypes.RoleProposer, spectypes.PostConsensusPartialSig, preGloasSlot))
	require.Equal(t, 1, mv.maxValidatorRoleSignatures(spectypes.RoleProposer, spectypes.RandaoPartialSig, gloasSlot))
	require.Equal(t, 1, mv.maxValidatorRoleSignatures(ssvtypes.RoleAggregator, spectypes.PostConsensusPartialSig, gloasSlot))
	require.Equal(t, 1, mv.maxValidatorRoleSignatures(spectypes.RolePTCAttester, spectypes.PTCAttesterPartialSig, gloasSlot))
	require.Equal(t, 1, mv.maxValidatorRoleSignatures(spectypes.RoleProposerPreferences, spectypes.ProposerPreferencesPartialSig, gloasSlot))
}
