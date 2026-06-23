package gloas

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestWireConstants pins the SIP-canonical wire values. These MUST match the Anchor
// client byte-for-byte — a change here is a cross-client wire break, not a refactor.
func TestWireConstants(t *testing.T) {
	require.Equal(t, spectypes.RunnerRole(7), RolePTCAttester)
	require.Equal(t, spectypes.RunnerRole(8), RoleProposerPreferences)
	require.Equal(t, spectypes.RunnerRole(9), RoleEnvelopeBuilder)

	require.Equal(t, spectypes.BeaconRole(7), BNRolePTCAttester)
	require.Equal(t, spectypes.BeaconRole(8), BNRoleProposerPreferences)
	require.Equal(t, spectypes.BeaconRole(9), BNRoleEnvelopeBuilder)

	require.Equal(t, spectypes.PartialSigMsgType(7), PTCAttesterPartialSig)
	require.Equal(t, spectypes.PartialSigMsgType(8), ProposerPreferencesPartialSig)

	require.Equal(t, [4]byte{0x0b, 0x00, 0x00, 0x00}, DomainBeaconBuilder)
	require.Equal(t, [4]byte{0x0c, 0x00, 0x00, 0x00}, DomainPTCAttester)
	require.Equal(t, [4]byte{0x0d, 0x00, 0x00, 0x00}, DomainProposerPreferences)
}

func TestRunnerRoleForBeaconRole(t *testing.T) {
	for _, tc := range []struct {
		name string
		role spectypes.BeaconRole
		want spectypes.RunnerRole
		ok   bool
	}{
		{"ptc", BNRolePTCAttester, RolePTCAttester, true},
		{"proposer_preferences", BNRoleProposerPreferences, RoleProposerPreferences, true},
		{"envelope", BNRoleEnvelopeBuilder, RoleEnvelopeBuilder, true},
		{"non_gloas", spectypes.BNRoleAttester, spectypes.RoleUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RunnerRoleForBeaconRole(tc.role)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
