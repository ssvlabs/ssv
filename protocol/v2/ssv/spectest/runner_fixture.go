package spectest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// fixRunnerForRun, and the helpers it depends on, live in this non-test file (rather than
// ssv_mapping_test.go) because committee_msg_processing_type.go (a non-test file) also calls
// fixRunnerForRun when fixing up AggregatorCommitteeRunners read out of state-comparison JSON;
// a non-test file cannot reference symbols defined only in a _test.go file under `go build`.
func fixRunnerForRun(
	t *testing.T,
	runnerMap map[string]any,
	ks *spectestingutils.TestKeySet,
	netCfg *networkconfig.Network,
) runner.Runner {
	logger := log.TestLogger(t)

	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

	baseRunner := &runner.BaseRunner{}
	byts, err := json.Marshal(baseRunnerMap)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(byts, &baseRunner))
	if netCfg == nil {
		netCfg = networkconfig.TestNetwork
	}
	// AggregatorCommitteeRunner always needs the Fulu fork pushed out (see testNetworkConfig),
	// regardless of what the caller passed in, since its consensus-value encoding depends on
	// ForkAtEpoch resolving to the fixture's expected fork (Electra) rather than Fulu; our
	// TestNetwork's Fulu fork epoch is low enough that fixture duty slots would otherwise resolve
	// post-Fulu.
	if baseRunner.RunnerRoleType == spectypes.RoleAggregatorCommittee {
		netCfg = testNetworkConfig(true)
	}
	baseRunner.NetworkConfig = netCfg

	ret := createRunnerWithBaseRunner(logger, baseRunner.RunnerRoleType, baseRunner, ks)

	if baseRunner.QBFTController != nil {
		baseRunner.QBFTController = fixControllerForRun(logger, baseRunner.QBFTController, ks)
		if baseRunner.HasStartedQBFTInstance() {
			operator := spectestingutils.TestingCommitteeMember(ks)
			baseRunner.State.RunningInstance = fixInstanceForRun(logger, ks, baseRunner.State.RunningInstance, baseRunner.QBFTController, operator)
		}
	}

	return ret
}

func fixControllerForRun(logger *zap.Logger, contr *controller.Controller, ks *spectestingutils.TestKeySet) *controller.Controller {
	config := protocoltesting.TestingConfig(logger, ks)
	newContr := controller.NewController(
		contr.Identifier,
		contr.CommitteeMember,
		config,
		spectestingutils.NewOperatorSigner(ks, 1),
		false,
	)
	newContr.LatestInstanceHeight = contr.LatestInstanceHeight
	newContr.RecentInstances = contr.RecentInstances

	for i, inst := range newContr.RecentInstances {
		if inst == nil {
			continue
		}
		operator := spectestingutils.TestingCommitteeMember(ks)
		newContr.RecentInstances[i] = fixInstanceForRun(logger, ks, inst, newContr, operator)
	}
	return newContr
}

func fixInstanceForRun(
	logger *zap.Logger,
	ks *spectestingutils.TestKeySet,
	inst *instance.Instance,
	contr *controller.Controller,
	share *spectypes.CommitteeMember,
) *instance.Instance {
	signer := spectestingutils.NewOperatorSigner(ks, 1)
	newInst := instance.NewInstance(
		context.Background(),
		logger,
		contr.GetConfig(),
		share,
		contr.GetIdentifier(),
		contr.LatestInstanceHeight,
		signer,
		func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
	)

	newInst.State.DecidedValue = inst.State.DecidedValue
	newInst.State.Decided = inst.State.Decided
	newInst.State.CommitteeMember = inst.State.CommitteeMember
	newInst.State.Round = inst.State.Round
	newInst.State.Height = inst.State.Height
	newInst.State.ProposalAcceptedForCurrentRound = inst.State.ProposalAcceptedForCurrentRound
	newInst.State.ID = inst.State.ID
	newInst.State.LastPreparedValue = inst.State.LastPreparedValue
	newInst.State.LastPreparedRound = inst.State.LastPreparedRound
	newInst.State.ProposeContainer = inst.State.ProposeContainer
	newInst.State.PrepareContainer = inst.State.PrepareContainer
	newInst.State.CommitContainer = inst.State.CommitContainer
	newInst.State.RoundChangeContainer = inst.State.RoundChangeContainer
	newInst.StartValue = inst.StartValue
	return newInst
}

func createRunnerWithBaseRunner(logger *zap.Logger, role spectypes.RunnerRole, base *runner.BaseRunner, ks *spectestingutils.TestKeySet) runner.Runner {
	switch role {
	case spectypes.RoleCommittee:
		ret := ssvtesting.CommitteeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	case ssvtypes.RoleAggregator:
		ret := ssvtesting.AggregatorRunner(logger, ks)
		ret.(*runner.AggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleProposer:
		ret := ssvtesting.ProposerRunner(logger, ks)
		ret.(*runner.ProposerRunner).BaseRunner = base
		return ret
	case ssvtypes.RoleSyncCommitteeContribution:
		ret := ssvtesting.SyncCommitteeContributionRunner(logger, ks)
		ret.(*runner.SyncCommitteeAggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleValidatorRegistration:
		ret := ssvtesting.ValidatorRegistrationRunner(logger, ks)
		ret.(*runner.ValidatorRegistrationRunner).BaseRunner = base
		return ret
	case spectypes.RoleAggregatorCommittee:
		ret := ssvtesting.AggregatorCommitteeRunner(logger, ks)
		ret.(*runner.AggregatorCommitteeRunner).BaseRunner = base
		return ret
	case spectypes.RoleVoluntaryExit:
		ret := ssvtesting.VoluntaryExitRunner(logger, ks)
		ret.(*runner.VoluntaryExitRunner).BaseRunner = base
		return ret
	case spectestingutils.UnknownDutyType:
		ret := ssvtesting.UnknownDutyTypeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	default:
		panic("unknown beacon role")
	}
}
