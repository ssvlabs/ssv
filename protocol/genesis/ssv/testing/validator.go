package testing

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

var BaseValidator = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())

	return validator.NewValidator(
		ctx,
		cancel,
		validator.Options{
			Network:       spectestingutils.NewTestingNetwork(),
			Beacon:        spectestingutils.NewTestingBeaconNode(),
			BeaconNetwork: networkconfig.TestNetwork.Beacon,
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
				spectypes.BNRoleValidatorRegistration:     ValidatorRegistrationRunner(logger, keySet),
				spectypes.BNRoleVoluntaryExit:             VoluntaryExitRunner(logger, keySet),
			},
		},
	)
}
