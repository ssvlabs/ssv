package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
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
	require.Equal(t, 8, mv.maxValidatorRoleSignatures(spectypes.RoleProposerPreferences, spectypes.RequestAuthPartialSig, gloasSlot))
}

// SIP #94 §7: every entry of a validator-role packet carries the same validator index (REJECT otherwise).
// The check runs before the membership rule, which stays an IGNORE, and committee packets — one entry
// per validator by design — are exempt.
func TestValidatePartialSignatureMessageSemantics_ValidatorIndexConsistency(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	packet := func(indices ...phase0.ValidatorIndex) *spectypes.PartialSignatureMessages {
		msgs := &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: gloasSlot}
		for _, idx := range indices {
			msgs.Messages = append(msgs.Messages, &spectypes.PartialSignatureMessage{Signer: 1, ValidatorIndex: idx})
		}
		return msgs
	}
	signed := func(role spectypes.RunnerRole) *spectypes.SignedSSVMessage {
		return &spectypes.SignedSSVMessage{
			OperatorIDs: []spectypes.OperatorID{1},
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				MsgID:   ssvtestingutils.NewMsgID(netCfg.DomainType, make([]byte, 48), role),
			},
		}
	}

	// The Gloas proposer's two-entry packet (block root + envelope root) for its one validator passes.
	require.NoError(t, mv.validatePartialSignatureMessageSemantics(signed(spectypes.RoleProposer), packet(7, 7), []phase0.ValidatorIndex{7}))

	// Entries for different validators make a malformed packet: REJECT, ahead of the membership IGNORE.
	err := mv.validatePartialSignatureMessageSemantics(signed(spectypes.RoleProposer), packet(7, 8), []phase0.ValidatorIndex{7})
	require.ErrorIs(t, err, ErrInconsistentValidatorIndex)
	kind, _ := classifyDiscard(err)
	require.Equal(t, discardRejected, kind)

	// A consistent index the packet's validator does not own is still only ignored (ssvlabs/knowledge-base#2).
	err = mv.validatePartialSignatureMessageSemantics(signed(spectypes.RoleProposer), packet(8, 8), []phase0.ValidatorIndex{7})
	require.ErrorIs(t, err, ErrValidatorIndexMismatch)
	kind, _ = classifyDiscard(err)
	require.Equal(t, discardIgnored, kind)

	// Committee packets legitimately carry one entry per validator.
	require.NoError(t, mv.validatePartialSignatureMessageSemantics(signed(spectypes.RoleCommittee), packet(7, 8), nil))
}
