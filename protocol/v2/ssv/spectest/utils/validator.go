package utils

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	validator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

var BaseValidator = func(keySet *testingutils.TestKeySet) *validator.Validator {
	return validator.NewValidator(
		validator.Options{
			Network: testingutils.NewTestingNetwork(),
			Beacon:  testingutils.NewTestingBeaconNode(),
			Storage: testingutils.NewTestingStorage(),
			Share:   testingutils.TestingShare(keySet),
			Signer:  testingutils.NewTestingKeyManager(),
			Runners: map[types.BeaconRole]runner.Runner{
				types.BNRoleAttester:                  AttesterRunner(keySet),
				types.BNRoleProposer:                  ProposerRunner(keySet),
				types.BNRoleAggregator:                AggregatorRunner(keySet),
				types.BNRoleSyncCommittee:             SyncCommitteeRunner(keySet),
				types.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(keySet),
			},
		},
	)
}
