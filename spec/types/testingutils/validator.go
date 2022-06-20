package testingutils

import (
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
)

var BaseValidator = func(keySet *TestKeySet) *ssv.Validator {
	ret := ssv.NewValidator(
		NewTestingNetwork(),
		NewTestingBeaconNode(),
		NewTestingStorage(),
		TestingShare(keySet),
		NewTestingKeyManager(),
	)
	ret.DutyRunners[types.BNRoleAttester] = AttesterRunner(keySet)
	ret.DutyRunners[types.BNRoleProposer] = ProposerRunner(keySet)
	ret.DutyRunners[types.BNRoleAggregator] = AggregatorRunner(keySet)
	ret.DutyRunners[types.BNRoleSyncCommittee] = SyncCommitteeRunner(keySet)
	ret.DutyRunners[types.BNRoleSyncCommitteeContribution] = SyncCommitteeContributionRunner(keySet)
	return ret
}
