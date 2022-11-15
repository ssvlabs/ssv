package utils

import (
	"context"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

var BaseValidator = func(keySet *testingutils.TestKeySet) *validator.Validator {
	return validator.NewValidator(
		context.TODO(),
		validator.Options{
			Network: testingutils.NewTestingNetwork(),
			Beacon:  testingutils.NewTestingBeaconNode(),
			Storage: testingutils.NewTestingStorage(),
			SSVShare: &types.SSVShare{
				Share: *testingutils.TestingShare(keySet),
			},
			Signer: testingutils.NewTestingKeyManager(),
			DutyRunners: map[spectypes.BeaconRole]runner.Runner{
				spectypes.BNRoleAttester:                  AttesterRunner(keySet),
				spectypes.BNRoleProposer:                  ProposerRunner(keySet),
				spectypes.BNRoleAggregator:                AggregatorRunner(keySet),
				spectypes.BNRoleSyncCommittee:             SyncCommitteeRunner(keySet),
				spectypes.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(keySet),
			},
		},
	)
}
