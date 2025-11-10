package spectest

import (
	"path/filepath"
	"testing"

	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	qbfttesting "github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
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
	default:
		t.Fatalf("unknown runner type")
	}
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	r, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, r)
	require.NoError(t, err)

	// override base-runner NetworkConfig now
	switch runnerType.(type) {
	case *runner.CommitteeRunner:
		r.(*runner.CommitteeRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
		r.(*runner.CommitteeRunner).ValCheck = qbfttesting.TestingValueChecker{}
		for _, inst := range r.(*runner.CommitteeRunner).BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = qbfttesting.TestingValueChecker{}
		}
		if r.(*runner.CommitteeRunner).BaseRunner.State != nil && r.(*runner.CommitteeRunner).BaseRunner.State.RunningInstance != nil {
			r.(*runner.CommitteeRunner).BaseRunner.State.RunningInstance.ValueChecker = qbfttesting.TestingValueChecker{}
		}
	case *runner.AggregatorRunner:
		r.(*runner.AggregatorRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
		r.(*runner.AggregatorRunner).ValCheck = qbfttesting.TestingValueChecker{}
		for _, inst := range r.(*runner.AggregatorRunner).BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = qbfttesting.TestingValueChecker{}
		}
		if r.(*runner.AggregatorRunner).BaseRunner.State != nil && r.(*runner.AggregatorRunner).BaseRunner.State.RunningInstance != nil {
			r.(*runner.AggregatorRunner).BaseRunner.State.RunningInstance.ValueChecker = qbfttesting.TestingValueChecker{}
		}
	case *runner.ProposerRunner:
		r.(*runner.ProposerRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
		r.(*runner.ProposerRunner).ValCheck = qbfttesting.TestingValueChecker{}
		for _, inst := range r.(*runner.ProposerRunner).BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = qbfttesting.TestingValueChecker{}
		}
		if r.(*runner.ProposerRunner).BaseRunner.State != nil && r.(*runner.ProposerRunner).BaseRunner.State.RunningInstance != nil {
			r.(*runner.ProposerRunner).BaseRunner.State.RunningInstance.ValueChecker = qbfttesting.TestingValueChecker{}
		}
	case *runner.SyncCommitteeAggregatorRunner:
		r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
		r.(*runner.SyncCommitteeAggregatorRunner).ValCheck = qbfttesting.TestingValueChecker{}
		for _, inst := range r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = qbfttesting.TestingValueChecker{}
		}
		if r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.State != nil && r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.State.RunningInstance != nil {
			r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.State.RunningInstance.ValueChecker = qbfttesting.TestingValueChecker{}
		}
	case *runner.ValidatorRegistrationRunner:
		r.(*runner.ValidatorRegistrationRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	case *runner.VoluntaryExitRunner:
		r.(*runner.VoluntaryExitRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	default:
		t.Fatalf("unknown runner type")
	}

	return r
}
