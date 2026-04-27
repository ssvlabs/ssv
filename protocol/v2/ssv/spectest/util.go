package spectest

import (
	"fmt"
	"path/filepath"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

// beaconNodeFromRunner extracts the beacon.BeaconNode from a runner.Runner.
// The Runner interface intentionally does not expose the beacon node, so spec
// tests that need direct access (e.g. to inspect broadcasted roots) must
// type-switch on the concrete runner type.
func beaconNodeFromRunner(r runner.Runner) beacon.BeaconNode {
	switch r := r.(type) {
	case *runner.CommitteeRunner:
		return r.Beacon
	case *runner.AggregatorRunner:
		return r.Beacon
	case *runner.ProposerRunner:
		return r.Beacon
	case *runner.SyncCommitteeAggregatorRunner:
		return r.Beacon
	case *runner.ValidatorRegistrationRunner:
		return r.Beacon
	case *runner.VoluntaryExitRunner:
		return r.Beacon
	}
	panic(fmt.Sprintf("unknown runner type: %T", r))
}

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
		for _, inst := range cr.QBFTController.RecentInstances {
			inst.ValueChecker = valCheck
		}
		if cr.HasStartedQBFTInstance() {
			cr.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.AggregatorRunner:
		ar := r.(*runner.AggregatorRunner)
		ar.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		ar.ValCheck = valCheck
		for _, inst := range ar.QBFTController.RecentInstances {
			inst.ValueChecker = valCheck
		}
		if ar.HasStartedQBFTInstance() {
			ar.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.ProposerRunner:
		pr := r.(*runner.ProposerRunner)
		pr.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		pr.ValCheck = valCheck
		for _, inst := range pr.QBFTController.RecentInstances {
			inst.ValueChecker = valCheck
		}
		if pr.HasStartedQBFTInstance() {
			pr.State.RunningInstance.ValueChecker = valCheck
		}
	case *runner.SyncCommitteeAggregatorRunner:
		scr := r.(*runner.SyncCommitteeAggregatorRunner)
		scr.NetworkConfig = networkconfig.TestNetwork
		valCheck := createValueChecker(r, runnerType)
		scr.ValCheck = valCheck
		for _, inst := range scr.QBFTController.RecentInstances {
			inst.ValueChecker = valCheck
		}
		if scr.HasStartedQBFTInstance() {
			scr.State.RunningInstance.ValueChecker = valCheck
		}
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
		if pr.HasStartedQBFTInstance() {
			state.RunningInstance.StartValue = normalizeProposerConsensusValue(state.RunningInstance.StartValue)
			if state.RunningInstance.State != nil {
				state.RunningInstance.State.LastPreparedValue = normalizeProposerConsensusValue(state.RunningInstance.State.LastPreparedValue)
				state.RunningInstance.State.DecidedValue = normalizeProposerConsensusValue(state.RunningInstance.State.DecidedValue)
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
