package testing

import (
	"context"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
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
			DutyRunners: map[genesisspectypes.BeaconRole]runner.Runner{
				genesisspectypes.BNRoleAttester:                  AttesterRunner(logger, keySet),
				genesisspectypes.BNRoleProposer:                  ProposerRunner(logger, keySet),
				genesisspectypes.BNRoleAggregator:                AggregatorRunner(logger, keySet),
				genesisspectypes.BNRoleSyncCommittee:             SyncCommitteeRunner(logger, keySet),
				genesisspectypes.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(logger, keySet),
				genesisspectypes.BNRoleValidatorRegistration:     ValidatorRegistrationRunner(logger, keySet),
				genesisspectypes.BNRoleVoluntaryExit:             VoluntaryExitRunner(logger, keySet),
			},
		},
	)
}
