package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// The three Gloas runner roles must pass the fork-independent validRoleUnion gate in
// validateSSVMessage (issue #2999: they were REJECTed there — with a peer penalty — before the
// per-slot validRoleAtSlot ever ran, so no §3/§5/§6 duty could reach quorum while every node's
// own messages looked healthy via the validateSelf bypass).
func TestValidateSSVMessage_GloasRolesPassRoleUnion(t *testing.T) {
	mv := &messageValidator{}
	for _, role := range []spectypes.RunnerRole{
		spectypes.RolePTCAttester,
		spectypes.RoleProposerPreferences,
		spectypes.RoleEnvelopeProposer,
	} {
		msg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType{}, make([]byte, 48), role),
			Data:    []byte{1},
		}
		require.NoError(t, mv.validateSSVMessage(msg), "role %d must pass the role union", role)
	}

	// Negative control: an out-of-union role still REJECTs at the same gate.
	bad := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RunnerRole(999)),
		Data:    []byte{1},
	}
	require.ErrorIs(t, mv.validateSSVMessage(bad), ErrInvalidRole)
}

// Lockstep between the two role registries: any role validRoleAtSlot admits at any slot must be
// in validRoleUnion, or the union gate rejects it before the per-slot check can ever run. This is
// the drift guard issue #2999 lacked — the union arrived from stage after the branch had already
// extended the fork-gated check, and nothing tied the two together. The sweep bound mirrors the
// role sweeps in protocol/v2/message and observability/utils: headroom over the spec's max value.
func TestValidRoleUnion_LockstepWithValidRoleAtSlot(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}

	slots := []phase0.Slot{
		0, // earliest fork era in the test config
		phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch), // pre-Gloas
		phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch),   // Gloas
	}
	for i := 0; i <= 31; i++ {
		role := spectypes.RunnerRole(i)
		for _, slot := range slots {
			if mv.validRoleAtSlot(role, slot) {
				require.True(t, mv.validRoleUnion(role),
					"role %d is admitted by validRoleAtSlot at slot %d but missing from validRoleUnion", role, slot)
			}
		}
	}
}
