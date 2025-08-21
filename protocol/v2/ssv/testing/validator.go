package testing

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

var BaseValidator = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())

	commonOpts := &validator.CommonOptions{
		NetworkConfig: networkconfig.TestNetwork,
		Network:       spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
		Beacon:        tests.NewTestingBeaconNodeWrapped(),
		Storage:       testing.TestingStores(logger),
		Signer:        ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
	}

	return validator.NewValidator(
		ctx,
		cancel,
		logger,
		commonOpts.NewOptions(
			&types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex),
			},
			spectestingutils.TestingCommitteeMember(keySet),
			map[spectypes.RunnerRole]runner.Runner{
				spectypes.RoleCommittee:                 CommitteeRunner(logger, keySet),
				spectypes.RoleProposer:                  ProposerRunner(logger, keySet),
				spectypes.RoleAggregator:                AggregatorRunner(logger, keySet),
				spectypes.RoleSyncCommitteeContribution: SyncCommitteeContributionRunner(logger, keySet),
				spectypes.RoleValidatorRegistration:     ValidatorRegistrationRunner(logger, keySet),
				spectypes.RoleVoluntaryExit:             VoluntaryExitRunner(logger, keySet),
			}),
	)
}
