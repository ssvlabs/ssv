package testing

import (
	"context"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

var BaseValidator = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())

	return validator.NewValidator(
		ctx,
		cancel,
		validator.Options{
			Network:       spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
			Beacon:        spectestingutils.NewTestingBeaconNode(),
			BeaconNetwork: networkconfig.TestNetwork.Beacon,
			Storage:       testing.TestingStores(logger),
			SSVShare: &types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex),
			},
			Signer:            spectestingutils.NewTestingKeyManager(),
			OperatorSigner:    spectestingutils.NewTestingOperatorSigner(keySet, 1),
			SignatureVerifier: spectestingutils.NewTestingVerifier(),
			DutyRunners: map[spectypes.RunnerRole]runner.Runner{
				spectypes.RoleCommittee:                 CommitteeRunner(logger, keySet),
				spectypes.RoleProposer:                  ProposerRunner(logger, keySet),
				spectypes.RoleAggregator:                AggregatorRunner(logger, keySet),
				spectypes.RoleSyncCommitteeContribution: SyncCommitteeContributionRunner(logger, keySet),
				spectypes.RoleValidatorRegistration:     ValidatorRegistrationRunner(logger, keySet),
				spectypes.RoleVoluntaryExit:             VoluntaryExitRunner(logger, keySet),
			},
		},
	)
}
