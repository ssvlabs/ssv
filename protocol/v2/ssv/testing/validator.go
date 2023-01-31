package testing

import (
	"context"

	"github.com/bloxapp/ssv/protocol/v2/qbft/testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

var BaseValidator = func(keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())
	return validator.NewValidator(
		ctx,
		cancel,
		validator.Options{
			Network: spectestingutils.NewTestingNetwork(),
			Beacon:  spectestingutils.NewTestingBeaconNode(),
			Storage: testing.TestingStores(),
			SSVShare: &types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet),
			},
			Signer: spectestingutils.NewTestingKeyManager(),
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
