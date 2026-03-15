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
		cr.BaseRunner.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		cr.ValCheck = valCheck
		for _, inst := range cr.BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = valCheck
		}
		if cr.BaseRunner.State != nil && cr.BaseRunner.State.RunningInstance != nil {
			cr.BaseRunner.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.AggregatorRunner:
		ar := r.(*runner.AggregatorRunner)
		ar.BaseRunner.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		ar.ValCheck = valCheck
		for _, inst := range ar.BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = valCheck
		}
		if ar.BaseRunner.State != nil && ar.BaseRunner.State.RunningInstance != nil {
			ar.BaseRunner.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.ProposerRunner:
		pr := r.(*runner.ProposerRunner)
		pr.BaseRunner.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		pr.ValCheck = valCheck
		for _, inst := range pr.BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = valCheck
		}
		if pr.BaseRunner.State != nil && pr.BaseRunner.State.RunningInstance != nil {
			pr.BaseRunner.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.SyncCommitteeAggregatorRunner:
		scr := r.(*runner.SyncCommitteeAggregatorRunner)
		scr.BaseRunner.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		scr.ValCheck = valCheck
		for _, inst := range scr.BaseRunner.QBFTController.StoredInstances {
			inst.ValueChecker = valCheck
		}
		if scr.BaseRunner.State != nil && scr.BaseRunner.State.RunningInstance != nil {
			scr.BaseRunner.State.RunningInstance.ValueChecker = valCheck
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

func normalizeExpectedProposerStartValues(pr *runner.ProposerRunner) {
	if pr == nil || pr.BaseRunner == nil {
		return
	}
	if state := pr.BaseRunner.State; state != nil {
		state.DecidedValue = normalizeProposerConsensusValue(state.DecidedValue)
		if state.RunningInstance != nil {
			state.RunningInstance.StartValue = normalizeProposerConsensusValue(state.RunningInstance.StartValue)
			if state.RunningInstance.State != nil {
				state.RunningInstance.State.LastPreparedValue = normalizeProposerConsensusValue(state.RunningInstance.State.LastPreparedValue)
				state.RunningInstance.State.DecidedValue = normalizeProposerConsensusValue(state.RunningInstance.State.DecidedValue)
			}
		}
	}
	if pr.BaseRunner.QBFTController == nil {
		return
	}
	for _, inst := range pr.BaseRunner.QBFTController.StoredInstances {
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
