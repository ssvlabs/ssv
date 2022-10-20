package utils

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	validator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

var BaseValidator = func(keySet *testingutils.TestKeySet) *validator.Validator {
	return validator.NewValidator(
		testingutils.NewTestingNetwork(),
		testingutils.NewTestingBeaconNode(),
		testingutils.NewTestingStorage(),
		testingutils.TestingShare(keySet),
		testingutils.NewTestingKeyManager(),
		map[types.BeaconRole]runner.Runner{
			types.BNRoleAttester:                  AttesterRunner(keySet),
			types.BNRoleProposer:                  ProposerRunner(keySet),
			types.BNRoleAggregator:                AggregatorRunner(keySet),
			types.BNRoleSyncCommittee:             SyncCommitteeRunner(keySet),
			types.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(keySet),
		},
	)
}
