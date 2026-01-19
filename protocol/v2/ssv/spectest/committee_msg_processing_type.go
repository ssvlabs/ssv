package spectest

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	eth2clientspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	spec "github.com/ssvlabs/ssv-spec/ssv"
	stests "github.com/ssvlabs/ssv-spec/ssv/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

type CommitteeSpecTest struct {
	Name                   string
	ParentName             string
	Committee              *validator.Committee
	Input                  []any // Can be a types.Duty or a *types.SignedSSVMessage
	PostDutyCommitteeRoot  string
	PostDutyCommittee      spectypes.Root `json:"-"` // Field is ignored by encoding/json
	OutputMessages         []*spectypes.PartialSignatureMessages
	BeaconBroadcastedRoots []string
	ExpectedErrorCode      int
}

func (test *CommitteeSpecTest) TestName() string {
	return test.Name
}

func (test *CommitteeSpecTest) FullName() string {
	return strings.ReplaceAll(test.ParentName+"_"+test.Name, " ", "_")
}

// RunAsPartOfMultiTest runs the test as part of a MultiCommitteeSpecTest
func (test *CommitteeSpecTest) RunAsPartOfMultiTest(t *testing.T) {
	logger := log.TestLogger(t)
	lastErr := test.runPreTesting(logger)
	spectests.AssertErrorCode(t, test.ExpectedErrorCode, lastErr)

	broadcastedMsgsCap := 0
	broadcastedRootsCap := 0
	for _, runner := range test.Committee.Runners {
		network := runner.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgsCap += len(network.BroadcastedMsgs)
		broadcastedRootsCap += len(beaconNetwork.GetBroadcastedRoots())
	}

	broadcastedMsgs := make([]*spectypes.SignedSSVMessage, 0, broadcastedMsgsCap)
	broadcastedRoots := make([]phase0.Root, 0, broadcastedRootsCap)
	for _, r := range test.Committee.Runners {
		network := r.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork := r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}

	for _, r := range test.Committee.AggregatorRunners {
		network := r.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork := r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}

	// test output message (in asynchronous order)
	spectestingutils.ComparePartialSignatureOutputMessagesInAsynchronousOrder(t, test.OutputMessages, broadcastedMsgs, test.Committee.CommitteeMember.Committee)

	spectestingutils.CompareBroadcastedBeaconMsgs(t, test.BeaconBroadcastedRoots, broadcastedRoots)

	// post root
	postRoot, err := test.Committee.GetRoot()
	require.NoError(t, err)

	if test.PostDutyCommitteeRoot != hex.EncodeToString(postRoot[:]) {
		diff := dumpState(t, test.Name, test.Committee, test.PostDutyCommittee)
		t.Errorf("post runner state not equal %s", diff)
	}
}

// Run as an individual test
func (test *CommitteeSpecTest) Run(t *testing.T) {
	test.overrideStateComparison(t)
	test.RunAsPartOfMultiTest(t)
}

func (test *CommitteeSpecTest) runPreTesting(logger *zap.Logger) error {
	var lastErr error

	for _, input := range test.Input {
		var err error
		switch input := input.(type) {
		case spectypes.Duty:
			_, _, err = test.Committee.StartDuty(context.TODO(), logger, input)
			if err != nil {
				lastErr = err
			}
		case *spectypes.SignedSSVMessage:
			msg, err := queue.DecodeSignedSSVMessage(input)
			if err != nil {
				return errors.Wrap(err, "failed to decode SignedSSVMessage")
			}

			err = test.Committee.ProcessMessage(context.TODO(), logger, msg)
			if err != nil {
				lastErr = err
			}
		default:
			panic("input is neither duty or SignedSSVMessage")
		}
	}

	return lastErr
}

func (test *CommitteeSpecTest) overrideStateComparison(t *testing.T) {
	strType := reflect.TypeOf(test).String()
	strType = strings.Replace(strType, "spectest.", "committee.", 1)
	overrideStateComparisonCommitteeSpecTest(t, test, test.Name, strType)
}

func (test *CommitteeSpecTest) GetPostState(logger *zap.Logger) (any, error) {
	lastErr := test.runPreTesting(logger)
	if lastErr != nil && test.ExpectedErrorCode == 0 {
		return nil, lastErr
	}

	return test.Committee, nil
}

type MultiCommitteeSpecTest struct {
	Name  string
	Tests []*CommitteeSpecTest
}

func (tests *MultiCommitteeSpecTest) TestName() string {
	return tests.Name
}

func (tests *MultiCommitteeSpecTest) Run(t *testing.T) {
	tests.overrideStateComparison(t)

	for _, test := range tests.Tests {
		t.Run(test.TestName(), func(t *testing.T) {
			test.ParentName = tests.Name
			test.RunAsPartOfMultiTest(t)
		})
	}
}

// overrideStateComparison overrides the post state comparison for all tests in the multi test
func (tests *MultiCommitteeSpecTest) overrideStateComparison(t *testing.T) {
	testsName := strings.ReplaceAll(tests.TestName(), " ", "_")
	for _, test := range tests.Tests {
		path := filepath.Join(testsName, test.TestName())
		strType := reflect.TypeOf(tests).String()
		strType = strings.Replace(strType, "spectest.", "committee.", 1)
		overrideStateComparisonCommitteeSpecTest(t, test, path, strType)
	}
}

func (tests *MultiCommitteeSpecTest) GetPostState(logger *zap.Logger) (any, error) {
	ret := make(map[string]spectypes.Root, len(tests.Tests))
	for _, test := range tests.Tests {
		err := test.runPreTesting(logger)
		if err != nil && !stests.MatchesErrorCode(test.ExpectedErrorCode, err) {
			return nil, fmt.Errorf(
				"(%s) expected error with code: %d, got error: %w",
				test.TestName(),
				test.ExpectedErrorCode,
				err,
			)
		}
		ret[test.Name] = test.Committee
	}
	return ret, nil
}

func overrideStateComparisonCommitteeSpecTest(t *testing.T, test *CommitteeSpecTest, name string, testType string) {
	specCommittee := &spec.Committee{}
	specDir, err := storage.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	specCommittee, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, specCommittee)

	require.NoError(t, err)
	committee := &validator.Committee{}
	committee, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, committee)
	require.NoError(t, err)

	committee.Shares = specCommittee.Share
	committee.CommitteeMember = &specCommittee.CommitteeMember

	// TODO: may be broken
	// Normalize: move any aggregator committee runners that may have been encoded under Runners into AggregatorRunners
	// to align with the current code structure.
	if committee.AggregatorRunners == nil {
		committee.AggregatorRunners = map[phase0.Slot]*runner.AggregatorCommitteeRunner{}
	}
	for slot, cr := range committee.Runners {
		if cr != nil && cr.BaseRunner != nil && cr.BaseRunner.RunnerRoleType == spectypes.RoleAggregatorCommittee {
			committee.AggregatorRunners[slot] = &runner.AggregatorCommitteeRunner{BaseRunner: cr.BaseRunner}
			delete(committee.Runners, slot)
		}
	}
	if test.Committee != nil {
		if test.Committee.AggregatorRunners == nil {
			test.Committee.AggregatorRunners = map[phase0.Slot]*runner.AggregatorCommitteeRunner{}
		}
		for slot, cr := range test.Committee.Runners {
			if cr != nil && cr.BaseRunner != nil && cr.BaseRunner.RunnerRoleType == spectypes.RoleAggregatorCommittee {
				test.Committee.AggregatorRunners[slot] = &runner.AggregatorCommitteeRunner{BaseRunner: cr.BaseRunner}
				delete(test.Committee.Runners, slot)
			}
		}
	}

	// Determine if this test involves aggregator committee duties/messages.
	needsAggRunners := false
	for _, in := range test.Input {
		switch v := in.(type) {
		case *spectypes.AggregatorCommitteeDuty:
			needsAggRunners = true
		case *spectypes.SignedSSVMessage:
			if v.SSVMessage != nil && v.SSVMessage.MsgID.GetRoleType() == spectypes.RoleAggregatorCommittee {
				needsAggRunners = true
			}
		}
		if needsAggRunners {
			break
		}
	}

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.Forks = maps.Clone(beaconCfg.Forks)
	fuluFork := beaconCfg.Forks[eth2clientspec.DataVersionFulu]
	fuluFork.Epoch = math.MaxUint64 // aggregator committee spec tests are implemented for Electra
	beaconCfg.Forks[eth2clientspec.DataVersionFulu] = fuluFork

	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg

	// Normalize runners/networks and set value checkers for both expected and actual committee runners.
	normalizeBaseRunner := func(base *runner.BaseRunner) {
		if base == nil {
			return
		}
		base.NetworkConfig = &netCfg
		// Ensure controller instances have a value checker.
		if base.QBFTController != nil {
			for _, inst := range base.QBFTController.StoredInstances {
				if inst.ValueChecker == nil {
					inst.ValueChecker = protocoltesting.TestingValueChecker{}
				}
			}
		}
		if base.State != nil && base.State.RunningInstance != nil && base.State.RunningInstance.ValueChecker == nil {
			base.State.RunningInstance.ValueChecker = protocoltesting.TestingValueChecker{}
		}
	}
	normalizeCommitteeRunner := func(cr *runner.CommitteeRunner) {
		if cr == nil || cr.BaseRunner == nil {
			return
		}
		normalizeBaseRunner(cr.BaseRunner)
		cr.ValCheck = protocoltesting.TestingValueChecker{}
	}
	normalizeAggregatorRunner := func(ar *runner.AggregatorCommitteeRunner) {
		if ar == nil || ar.BaseRunner == nil {
			return
		}
		normalizeBaseRunner(ar.BaseRunner)
		ar.ValCheck = protocoltesting.TestingValueChecker{}
	}

	for i := range committee.Runners {
		normalizeCommitteeRunner(committee.Runners[i])
	}
	for i := range test.Committee.Runners {
		normalizeCommitteeRunner(test.Committee.Runners[i])
	}

	if needsAggRunners {
		// Normalize existing aggregator runners on both sides without synthesizing new ones.
		for i := range committee.AggregatorRunners {
			normalizeAggregatorRunner(committee.AggregatorRunners[i])
		}
		for i := range test.Committee.AggregatorRunners {
			normalizeAggregatorRunner(test.Committee.AggregatorRunners[i])
		}
	}

	if test.Committee != nil && test.Committee.CreateRunnerFn != nil {
		origCreateRunner := test.Committee.CreateRunnerFn
		test.Committee.CreateRunnerFn = func(
			duty spectypes.Duty,
			shareMap map[phase0.ValidatorIndex]*spectypes.Share,
			attestingValidators []phase0.BLSPubKey,
			dutyGuard runner.CommitteeDutyGuard,
		) (runner.Runner, error) {
			r, err := origCreateRunner(duty, shareMap, attestingValidators, dutyGuard)
			if err != nil {
				return nil, err
			}
			switch created := r.(type) {
			case *runner.CommitteeRunner:
				normalizeCommitteeRunner(created)
			case *runner.AggregatorCommitteeRunner:
				normalizeAggregatorRunner(created)
			}
			return r, nil
		}
	}

	// Final normalization: ensure Runners contains only RoleCommittee runners on both sides.
	// Move any stray RoleAggregatorCommittee entries into AggregatorRunners.
	{
		filtered := make(map[phase0.Slot]*runner.CommitteeRunner, len(committee.Runners))
		for slot, cr := range committee.Runners {
			if cr != nil && cr.BaseRunner != nil && cr.BaseRunner.RunnerRoleType == spectypes.RoleAggregatorCommittee {
				if committee.AggregatorRunners == nil {
					committee.AggregatorRunners = map[phase0.Slot]*runner.AggregatorCommitteeRunner{}
				}
				committee.AggregatorRunners[slot] = &runner.AggregatorCommitteeRunner{BaseRunner: cr.BaseRunner}
				continue
			}
			filtered[slot] = cr
		}
		committee.Runners = filtered
	}
	if test.Committee != nil {
		filtered := make(map[phase0.Slot]*runner.CommitteeRunner, len(test.Committee.Runners))
		for slot, cr := range test.Committee.Runners {
			if cr != nil && cr.BaseRunner != nil && cr.BaseRunner.RunnerRoleType == spectypes.RoleAggregatorCommittee {
				if test.Committee.AggregatorRunners == nil {
					test.Committee.AggregatorRunners = map[phase0.Slot]*runner.AggregatorCommitteeRunner{}
				}
				test.Committee.AggregatorRunners[slot] = &runner.AggregatorCommitteeRunner{BaseRunner: cr.BaseRunner}
				continue
			}
			filtered[slot] = cr
		}
		test.Committee.Runners = filtered
	}

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])

	test.PostDutyCommittee = committee
}
