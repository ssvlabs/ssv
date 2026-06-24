package validation

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

func TestPartialSignatureTypeMatchesRole_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.partialSignatureTypeMatchesRole(spectypes.ProposerPreferencesPartialSig, spectypes.RoleProposerPreferences))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.PostConsensusPartialSig, spectypes.RoleProposerPreferences))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.ProposerPreferencesPartialSig, spectypes.RolePTCAttester))
}

func TestValidPartialSigMsgType_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.validPartialSigMsgType(spectypes.ProposerPreferencesPartialSig))
}

// ProposerPreferences partial sigs ride the future proposal slot, so validateSlotTime must allow them
// up to the proposer-lookahead window early — but no other role, and not beyond the window.
func TestValidateSlotTime_ProposerPreferencesEarliness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slot := phase0.Slot(1000)
	allowance := time.Duration(proposerPreferencesEarlyEpochs*netCfg.SlotsPerEpoch) * netCfg.SlotDuration

	tt := []struct {
		name     string
		role     spectypes.RunnerRole
		earlyBy  time.Duration
		accepted bool
	}{
		{"preferences within the lookahead window", spectypes.RoleProposerPreferences, allowance - time.Second, true},
		{"preferences beyond the lookahead window", spectypes.RoleProposerPreferences, allowance + time.Minute, false},
		{"another role gets no early allowance", spectypes.RoleProposer, allowance - time.Second, false},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			receivedAt := netCfg.SlotStartTime(slot).Add(-tc.earlyBy)
			err := mv.validateSlotTime(slot, tc.role, receivedAt)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrEarlySlotMessage)
			}
		})
	}
}

// ProposerPreferences is exempt from the monotonic slot-advance rule (a signer holds its whole
// lookahead at once); other validator roles still enforce it.
func TestMonotonicSlotRole_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.False(t, mv.monotonicSlotRole(spectypes.RoleProposerPreferences))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleProposer))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleValidatorRegistration))
	require.False(t, mv.monotonicSlotRole(spectypes.RoleCommittee))
}

func TestStoredSlotCount_ProposerPreferences(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	require.Equal(t, mv.maxStoredSlots(), mv.storedSlotCount(spectypes.RoleProposer))
	require.Equal(t,
		proposerPreferencesEarlyEpochs*netCfg.SlotsPerEpoch+mv.maxStoredSlots(),
		mv.storedSlotCount(spectypes.RoleProposerPreferences))
}

// Two proposal slots exactly one default-ring apart collide in the default ring but stay distinct in
// the lookahead-sized proposer-preferences ring, keeping per-slot dedup exact.
func TestProposerPreferencesRingAvoidsLookaheadCollision(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slotA := phase0.Slot(1000)
	slotB := slotA + phase0.Slot(mv.maxStoredSlots()) // collides with slotA in the default ring

	osDefault := newOperatorState(mv.maxStoredSlots())
	osDefault.SetSignerStateForSlot(slotA, 0, &SignerStateForSlotRound{Slot: slotA})
	osDefault.SetSignerStateForSlot(slotB, 0, &SignerStateForSlotRound{Slot: slotB})
	require.Nil(t, osDefault.GetSignerStateForSlot(slotA), "default ring should drop slotA on collision")

	osPrefs := newOperatorState(mv.storedSlotCount(spectypes.RoleProposerPreferences))
	osPrefs.SetSignerStateForSlot(slotA, 0, &SignerStateForSlotRound{Slot: slotA})
	osPrefs.SetSignerStateForSlot(slotB, 0, &SignerStateForSlotRound{Slot: slotB})
	require.NotNil(t, osPrefs.GetSignerStateForSlot(slotA))
	require.NotNil(t, osPrefs.GetSignerStateForSlot(slotB))
}

// With the monotonic check skipped, lateness is the role's replay bound: a preference around its
// proposal slot is fine, one for a slot well behind is late.
func TestMessageLateness_ProposerPreferences(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}
	slot := phase0.Slot(1000)

	notLate := mv.messageLateness(slot, spectypes.RoleProposerPreferences, netCfg.SlotStartTime(slot))
	require.LessOrEqual(t, notLate, time.Duration(0))

	late := mv.messageLateness(slot, spectypes.RoleProposerPreferences, netCfg.SlotStartTime(slot+100))
	require.Greater(t, late, time.Duration(0))
}

// ValidatorRegistration is deprecated at the Gloas fork — valid pre-Gloas, rejected for Gloas slots.
func TestValidRoleAtSlot_ValidatorRegistrationDeprecatedAtGloas(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}

	preGloasSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	require.True(t, mv.validRoleAtSlot(spectypes.RoleValidatorRegistration, preGloasSlot))
	require.False(t, mv.validRoleAtSlot(spectypes.RoleValidatorRegistration, gloasSlot))
}
