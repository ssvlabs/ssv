package types

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestCommitteeRunnerRoleForBeaconRole(t *testing.T) {
	testCases := []struct {
		name     string
		role     spectypes.BeaconRole
		wantRole spectypes.RunnerRole
		wantOK   bool
	}{
		{name: "attester maps to committee", role: spectypes.BNRoleAttester, wantRole: spectypes.RoleCommittee, wantOK: true},
		{name: "sync committee maps to committee", role: spectypes.BNRoleSyncCommittee, wantRole: spectypes.RoleCommittee, wantOK: true},
		{name: "aggregator maps to aggregator committee", role: spectypes.BNRoleAggregator, wantRole: spectypes.RoleAggregatorCommittee, wantOK: true},
		{name: "sync committee contribution maps to aggregator committee", role: spectypes.BNRoleSyncCommitteeContribution, wantRole: spectypes.RoleAggregatorCommittee, wantOK: true},
		{name: "proposer is not committee-backed", role: spectypes.BNRoleProposer, wantOK: false},
		{name: "validator registration is not committee-backed", role: spectypes.BNRoleValidatorRegistration, wantOK: false},
		{name: "voluntary exit is not committee-backed", role: spectypes.BNRoleVoluntaryExit, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CommitteeRunnerRoleForBeaconRole(tc.role)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantRole, got)
			}
		})
	}
}

func TestCommitteeSignerBucketForBeaconRole(t *testing.T) {
	testCases := []struct {
		name       string
		role       spectypes.BeaconRole
		wantBucket CommitteeSignerBucket
		wantOK     bool
	}{
		{name: "attester maps to attester bucket", role: spectypes.BNRoleAttester, wantBucket: CommitteeSignerBucketAttester, wantOK: true},
		{name: "aggregator maps to attester bucket", role: spectypes.BNRoleAggregator, wantBucket: CommitteeSignerBucketAttester, wantOK: true},
		{name: "sync committee maps to sync committee bucket", role: spectypes.BNRoleSyncCommittee, wantBucket: CommitteeSignerBucketSyncCommittee, wantOK: true},
		{name: "sync committee contribution maps to sync committee bucket", role: spectypes.BNRoleSyncCommitteeContribution, wantBucket: CommitteeSignerBucketSyncCommittee, wantOK: true},
		{name: "proposer is not a committee signer bucket", role: spectypes.BNRoleProposer, wantOK: false},
		{name: "voluntary exit is not a committee signer bucket", role: spectypes.BNRoleVoluntaryExit, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CommitteeSignerBucketForBeaconRole(tc.role)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantBucket, got)
			} else {
				assert.Equal(t, CommitteeSignerBucketUnknown, got)
			}
		})
	}
}

func TestRunnerRoleForDuty_CommitteeDuty(t *testing.T) {
	// CommitteeRunnerRoleForBeaconRole must be self-consistent with the runner-role
	// constants used elsewhere in the codebase to key committee traces.
	role, ok := CommitteeRunnerRoleForBeaconRole(spectypes.BNRoleAttester)
	require.True(t, ok)
	assert.Equal(t, spectypes.RoleCommittee, role)

	role, ok = CommitteeRunnerRoleForBeaconRole(spectypes.BNRoleAggregator)
	require.True(t, ok)
	assert.Equal(t, spectypes.RoleAggregatorCommittee, role)
}

func TestRunnerRoleForValidatorDuty_Gloas(t *testing.T) {
	duty := &spectypes.ValidatorDuty{Type: gloas.BNRolePTCAttester}
	require.Equal(t, gloas.RolePTCAttester, RunnerRoleForValidatorDuty(duty, true))
	require.Equal(t, spectypes.RoleUnknown, RunnerRoleForValidatorDuty(nil, true))

	// Non-Gloas roles still resolve via ssv-spec's RunnerRole().
	proposer := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer}
	require.Equal(t, spectypes.RoleProposer, RunnerRoleForValidatorDuty(proposer, true))
}
