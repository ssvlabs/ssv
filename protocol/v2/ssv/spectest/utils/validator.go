package utils

import (
	"context"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// BaseValidator creates a new Validator.
var BaseValidator = func(keySet *spectestingutils.TestKeySet) *validator.Validator {
	return validator.NewValidator(
		context.TODO(),
		validator.Options{
			Network: spectestingutils.NewTestingNetwork(),
			Beacon:  spectestingutils.NewTestingBeaconNode(),
			Storage: TestingStorage(),
			SSVShare: &types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet),
			},
			Signer: spectestingutils.NewTestingKeyManager(),
			DutyRunners: map[spectypes.BeaconRole]specssv.Runner{
				spectypes.BNRoleAttester:                  spectestingutils.AttesterRunner(keySet),
				spectypes.BNRoleProposer:                  spectestingutils.ProposerRunner(keySet),
				spectypes.BNRoleAggregator:                spectestingutils.AggregatorRunner(keySet),
				spectypes.BNRoleSyncCommittee:             spectestingutils.SyncCommitteeRunner(keySet),
				spectypes.BNRoleSyncCommitteeContribution: spectestingutils.SyncCommitteeContributionRunner(keySet),
			},
		},
	)
}
