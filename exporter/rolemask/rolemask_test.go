package rolemask

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
)

func TestAllContainsCanonicalRoles(t *testing.T) {
	roles := All()
	assert.Equal(t, []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleProposer,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRolePTCAttester,
		spectypes.BNRoleProposerPreferences,
	}, roles)
}

func TestBitOfAndHas(t *testing.T) {
	bit, ok := BitOf(spectypes.BNRoleAttester)
	assert.True(t, ok)
	assert.Equal(t, BitAttester, bit)

	mask := BitAttester | BitProposer | BitProposerPreferences
	assert.True(t, Has(mask, spectypes.BNRoleAttester))
	assert.True(t, Has(mask, spectypes.BNRoleProposer))
	assert.True(t, Has(mask, spectypes.BNRoleProposerPreferences))
	assert.False(t, Has(mask, spectypes.BNRoleAggregator))
	assert.False(t, Has(mask, spectypes.BNRolePTCAttester))

	_, ok = BitOf(spectypes.BeaconRole(255))
	assert.False(t, ok)
	assert.False(t, Has(mask, spectypes.BeaconRole(255)))
}
