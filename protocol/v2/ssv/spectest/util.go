package spectest

import (
	"path/filepath"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
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
	default:
		t.Fatalf("unknown runner type")
	}
	specDir, err := storage.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	r, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, r)
	require.NoError(t, err)

	// override base-runner NetworkConfig now
	// Pass runnerType as signerSource since it has the signer (r was deserialized and lacks one)
	switch runnerType.(type) {
	case *runner.CommitteeRunner:
		cr := r.(*runner.CommitteeRunner)
		cr.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		cr.ValCheck = valCheck
		rehydrateRunnerQBFTValueCheckers(cr, valCheck, true)
	case *runner.AggregatorRunner:
		ar := r.(*runner.AggregatorRunner)
		ar.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		ar.ValCheck = valCheck
		rehydrateRunnerQBFTValueCheckers(ar, valCheck, true)
	case *runner.ProposerRunner:
		pr := r.(*runner.ProposerRunner)
		pr.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		pr.ValCheck = valCheck
		rehydrateRunnerQBFTValueCheckers(pr, valCheck, true)
	case *runner.SyncCommitteeAggregatorRunner:
		scr := r.(*runner.SyncCommitteeAggregatorRunner)
		scr.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		scr.ValCheck = valCheck
		rehydrateRunnerQBFTValueCheckers(scr, valCheck, true)
	case *runner.ValidatorRegistrationRunner:
		r.(*runner.ValidatorRegistrationRunner).NetworkConfig = networkconfig.TestNetwork
	case *runner.VoluntaryExitRunner:
		r.(*runner.VoluntaryExitRunner).NetworkConfig = networkconfig.TestNetwork
	default:
		t.Fatalf("unknown runner type")
	}

	return r
}

func normalizeExpectedProposerStartValues(pr *runner.ProposerRunner) {
	if pr == nil || pr.BaseRunner == nil {
		return
	}
	if state := pr.State; state != nil {
		state.DecidedValue = normalizeProposerConsensusValue(state.DecidedValue)
		if currentInstance := pr.CurrentInstance(); currentInstance != nil {
			currentInstance.StartValue = normalizeProposerConsensusValue(currentInstance.StartValue)
			if currentInstance.State != nil {
				currentInstance.State.LastPreparedValue = normalizeProposerConsensusValue(currentInstance.State.LastPreparedValue)
				currentInstance.State.DecidedValue = normalizeProposerConsensusValue(currentInstance.State.DecidedValue)
			}
		}
	}
	if pr.QBFTController == nil {
		return
	}
	for _, inst := range pr.QBFTController.RecentInstances {
		if inst == nil {
			continue
		}
		inst.StartValue = normalizeProposerConsensusValue(inst.StartValue)
		if inst.State != nil {
			inst.State.LastPreparedValue = normalizeProposerConsensusValue(inst.State.LastPreparedValue)
			inst.State.DecidedValue = normalizeProposerConsensusValue(inst.State.DecidedValue)
		}
	}
}

func normalizeProposerConsensusValue(value []byte) []byte {
	if len(value) == 0 {
		return value
	}
	cd := &spectypes.ValidatorConsensusData{}
	if err := cd.Decode(value); err != nil {
		return value
	}
	vBlk, _, err := cd.GetBlockData()
	if err != nil {
		return value
	}
	blindedVBlk, blindedMarshaler, err := blindutil.EnsureBlinded(vBlk)
	if err != nil {
		return value
	}
	blindedDataSSZ, err := blindedMarshaler.MarshalSSZ()
	if err != nil {
		return value
	}
	cd.Version = blindedVBlk.Version
	cd.DataSSZ = blindedDataSSZ
	encoded, err := cd.Encode()
	if err != nil {
		return value
	}
	return encoded
}

func rehydrateRunnerQBFTValueCheckers(r runner.Runner, valCheck ssv.ValueChecker, overwrite bool) {
	if valCheck == nil {
		return
	}

	for _, inst := range qbftInstancesForRunner(r) {
		if overwrite || inst.ValueChecker == nil {
			inst.ValueChecker = valCheck
		}
	}
}

func qbftInstancesForRunner(r runner.Runner) []*instance.Instance {
	base := baseRunnerForSpectest(r)
	if base == nil || base.QBFTController == nil {
		return nil
	}

	instances := make([]*instance.Instance, 0, len(base.QBFTController.RecentInstances)+1)
	seen := make(map[*instance.Instance]struct{}, len(base.QBFTController.RecentInstances)+1)
	add := func(inst *instance.Instance) {
		if inst == nil {
			return
		}
		if _, ok := seen[inst]; ok {
			return
		}
		seen[inst] = struct{}{}
		instances = append(instances, inst)
	}

	for _, inst := range base.QBFTController.RecentInstances {
		add(inst)
	}
	add(base.CurrentInstance())

	return instances
}

func baseRunnerForSpectest(r runner.Runner) *runner.BaseRunner {
	switch typedRunner := r.(type) {
	case *runner.CommitteeRunner:
		return typedRunner.BaseRunner
	case *runner.AggregatorRunner:
		return typedRunner.BaseRunner
	case *runner.ProposerRunner:
		return typedRunner.BaseRunner
	case *runner.SyncCommitteeAggregatorRunner:
		return typedRunner.BaseRunner
	default:
		return nil
	}
}
