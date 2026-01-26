package spectest

import (
	"path/filepath"
	"testing"

	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

func runnerForTest(t *testing.T, runnerType runner.Runner, name string, testType string) runner.Runner {
	var r runner.Runner

	switch runnerType.(type) {
	case *runner.CommitteeRunner:
		r = &runner.CommitteeRunner{}
	case *runner.AggregatorRunner:
		r = &runner.AggregatorRunner{}
	case *runner.ProposerRunner:
		r = &runner.ProposerRunner{}
	case *runner.SyncCommitteeAggregatorRunner:
		r = &runner.SyncCommitteeAggregatorRunner{}
	case *runner.ValidatorRegistrationRunner:
		r = &runner.ValidatorRegistrationRunner{}
	case *runner.VoluntaryExitRunner:
		r = &runner.VoluntaryExitRunner{}
	case *runner.AggregatorCommitteeRunner:
		r = &runner.AggregatorCommitteeRunner{}
	default:
		t.Fatalf("unknown runner type")
	}
	specDir, err := storage.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	r, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, r)
	require.NoError(t, err)

	// override base-runner NetworkConfig now
	// Pass runnerType as signerSource since it has the signer (r was deserialized and lacks one)
	applyRunnerNetworkConfig(r, networkconfig.TestNetwork)
	switch runnerType.(type) {
	case *runner.CommitteeRunner, *runner.AggregatorRunner, *runner.ProposerRunner, *runner.SyncCommitteeAggregatorRunner:
		valCheck := createValueChecker(r, runnerType)
		setRunnerValCheck(r, valCheck)
		setRunnerValueCheckers(r, valCheck)
	case *runner.ValidatorRegistrationRunner, *runner.VoluntaryExitRunner, *runner.AggregatorCommitteeRunner:
	default:
		t.Fatalf("unknown runner type")
	}

	return r
}

func runnerBase(r runner.Runner) *runner.BaseRunner {
	switch typed := r.(type) {
	case *runner.CommitteeRunner:
		return typed.BaseRunner
	case *runner.AggregatorRunner:
		return typed.BaseRunner
	case *runner.ProposerRunner:
		return typed.BaseRunner
	case *runner.SyncCommitteeAggregatorRunner:
		return typed.BaseRunner
	case *runner.ValidatorRegistrationRunner:
		return typed.BaseRunner
	case *runner.VoluntaryExitRunner:
		return typed.BaseRunner
	case *runner.AggregatorCommitteeRunner:
		return typed.BaseRunner
	default:
		return nil
	}
}

func applyRunnerNetworkConfig(r runner.Runner, netCfg *networkconfig.Network) {
	base := runnerBase(r)
	if base == nil || netCfg == nil {
		return
	}
	base.NetworkConfig = netCfg
}

func runnerSupportsValueCheckers(r runner.Runner) bool {
	switch r.(type) {
	case *runner.CommitteeRunner, *runner.AggregatorRunner, *runner.ProposerRunner, *runner.SyncCommitteeAggregatorRunner:
		return true
	default:
		return false
	}
}

func setRunnerValCheck(r runner.Runner, valCheck ssv.ValueChecker) {
	if valCheck == nil {
		return
	}
	switch typed := r.(type) {
	case *runner.CommitteeRunner:
		typed.ValCheck = valCheck
	case *runner.AggregatorRunner:
		typed.ValCheck = valCheck
	case *runner.ProposerRunner:
		typed.ValCheck = valCheck
	case *runner.SyncCommitteeAggregatorRunner:
		typed.ValCheck = valCheck
	}
}

func setRunnerValueCheckers(r runner.Runner, valCheck ssv.ValueChecker) {
	if valCheck == nil || !runnerSupportsValueCheckers(r) {
		return
	}
	base := runnerBase(r)
	if base == nil || base.QBFTController == nil {
		return
	}
	for _, inst := range base.QBFTController.StoredInstances {
		if inst == nil {
			continue
		}
		inst.ValueChecker = valCheck
	}
	if base.State != nil && base.State.RunningInstance != nil {
		base.State.RunningInstance.ValueChecker = valCheck
	}
}

func setRunnerValueCheckersIfNil(r runner.Runner, valCheck ssv.ValueChecker) {
	if valCheck == nil || !runnerSupportsValueCheckers(r) {
		return
	}
	base := runnerBase(r)
	if base == nil || base.QBFTController == nil {
		return
	}
	for _, inst := range base.QBFTController.StoredInstances {
		if inst == nil || inst.ValueChecker != nil {
			continue
		}
		inst.ValueChecker = valCheck
	}
	if base.State != nil && base.State.RunningInstance != nil && base.State.RunningInstance.ValueChecker == nil {
		base.State.RunningInstance.ValueChecker = valCheck
	}
}
