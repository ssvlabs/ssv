package testing

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var BaseValidator = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) *validator.Validator {
	ctx, cancel := context.WithCancel(context.TODO())

	return validator.NewValidator(
		ctx,
		cancel,
		validator.Options{
			Network:       spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1]),
			NetworkConfig: networkconfig.TestNetwork,
			Beacon:        tests.NewTestingBeaconNodeWrapped(),
			Storage:       testing.TestingStores(logger),
			SSVShare: &types.SSVShare{
				Share: *spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex),
			},
			Signer:   ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			Operator: spectestingutils.TestingCommitteeMember(keySet),
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
