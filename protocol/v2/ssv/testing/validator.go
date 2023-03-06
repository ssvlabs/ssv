package testing

import (
	"context"

	"github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	"go.uber.org/zap"

	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

var BaseValidator = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())
	return validator.NewValidator(
		ctx,
		cancel,
		validator.Options{
			Network: spectestingutils.NewTestingNetwork(),
			Beacon:  spectestingutils.NewTestingBeaconNode(),
			Storage: testing.TestingStores(logger),
			SSVShare: &types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet),
			},
			Signer: spectestingutils.NewTestingKeyManager(),
			DutyRunners: map[spectypes.BeaconRole]runner.Runner{
				spectypes.BNRoleAttester:                  AttesterRunner(logger, keySet),
				spectypes.BNRoleProposer:                  ProposerRunner(logger, keySet),
				spectypes.BNRoleAggregator:                AggregatorRunner(logger, keySet),
				spectypes.BNRoleSyncCommittee:             SyncCommitteeRunner(logger, keySet),
				spectypes.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(logger, keySet),
			},
		},
	)
}
