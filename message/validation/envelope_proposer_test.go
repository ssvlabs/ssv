package validation

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// The §6 envelope duty is QBFT with only a post-consensus partial signature (no pre-consensus phase).
func TestPartialSignatureTypeMatchesRole_EnvelopeProposer(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.partialSignatureTypeMatchesRole(spectypes.PostConsensusPartialSig, spectypes.RoleEnvelopeProposer))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.RandaoPartialSig, spectypes.RoleEnvelopeProposer))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.ProposerPreferencesPartialSig, spectypes.RoleEnvelopeProposer))
}

// The envelope role exists only from the Gloas fork onward.
func TestValidRoleAtSlot_EnvelopeProposerGloasOnly(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}

	preGloasSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	require.False(t, mv.validRoleAtSlot(spectypes.RoleEnvelopeProposer, preGloasSlot))
	require.True(t, mv.validRoleAtSlot(spectypes.RoleEnvelopeProposer, gloasSlot))
}

// The envelope is a QBFT role (it has a max round) and shares the proposer's tight bound.
func TestMaxRound_EnvelopeProposer(t *testing.T) {
	mv := &messageValidator{}
	round, err := mv.maxRound(spectypes.RoleEnvelopeProposer)
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), round)
}

// At most one self-build proposal (hence one envelope) per slot → at most SlotsPerEpoch per epoch.
func TestDutyLimit_EnvelopeProposer(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	msgID := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RoleEnvelopeProposer)

	limit, ok := mv.dutyLimit(msgID, 0, nil)
	require.True(t, ok)
	require.Equal(t, mv.netCfg.SlotsPerEpoch, limit)
}

// The envelope signer advances one slot at a time, so a message for a slot below its max is stale.
func TestMonotonicSlotRole_EnvelopeProposer(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.monotonicSlotRole(spectypes.RoleEnvelopeProposer))
}

// An envelope message must carry a real proposer assignment once the slot's epoch is fetched AND
// fresh; unfetched and stale (mid-indices-change) epochs are tolerated, mirroring the
// proposer-preferences rule.
func TestValidateBeaconDuty_EnvelopeProposerAssignmentFreshness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	const epoch = phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	slot := phase0.Slot(uint64(epoch)*netCfg.SlotsPerEpoch + 3)

	ds := dutystore.New()
	assigned := []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
		{Slot: slot, ValidatorIndex: idx, Duty: &eth2apiv1.ProposerDuty{Slot: slot, ValidatorIndex: idx}, InCommittee: true},
	}
	ds.Proposer.Set(epoch, assigned)
	mv := &messageValidator{netCfg: netCfg, dutyStore: ds}

	indices := []phase0.ValidatorIndex{idx}
	require.NoError(t, mv.validateBeaconDuty(spectypes.RoleEnvelopeProposer, slot, indices, false))
	require.ErrorIs(t, mv.validateBeaconDuty(spectypes.RoleEnvelopeProposer, slot+1, indices, false), ErrNoDuty)

	ds.Proposer.MarkEpochsStale(epoch)
	require.NoError(t, mv.validateBeaconDuty(spectypes.RoleEnvelopeProposer, slot+1, indices, false))
	ds.Proposer.Set(epoch, assigned)
	require.ErrorIs(t, mv.validateBeaconDuty(spectypes.RoleEnvelopeProposer, slot+1, indices, false), ErrNoDuty)
}
