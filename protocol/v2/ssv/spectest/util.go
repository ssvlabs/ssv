package spectest

import (
	"path/filepath"
	"testing"

	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
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
	case *runner.AggregatorRunner:
		r.(*runner.AggregatorRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	case *runner.ProposerRunner:
		r.(*runner.ProposerRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	case *runner.SyncCommitteeAggregatorRunner:
		r.(*runner.SyncCommitteeAggregatorRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	case *runner.ValidatorRegistrationRunner:
		r.(*runner.ValidatorRegistrationRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	case *runner.VoluntaryExitRunner:
		r.(*runner.VoluntaryExitRunner).BaseRunner.NetworkConfig = networkconfig.TestNetwork
	default:
		t.Fatalf("unknown runner type")
	}

	return r
}
