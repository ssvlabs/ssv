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

	broadcastedMsgs := make([]*spectypes.SignedSSVMessage, 0)
	broadcastedRoots := make([]phase0.Root, 0)
	for _, r := range test.Committee.Runners {
		if net := r.GetNetwork(); net != nil {
			if tn, ok := net.(*spectestingutils.TestingNetwork); ok {
				broadcastedMsgs = append(broadcastedMsgs, tn.BroadcastedMsgs...)
			}
		}
		if bn := r.GetBeaconNode(); bn != nil {
			if bw, ok := bn.(*protocoltesting.BeaconNodeWrapped); ok {
				broadcastedRoots = append(broadcastedRoots, bw.GetBroadcastedRoots()...)
			}
		}
	}

	for _, r := range test.Committee.AggregatorRunners {
		if net := r.GetNetwork(); net != nil {
			if tn, ok := net.(*spectestingutils.TestingNetwork); ok {
				broadcastedMsgs = append(broadcastedMsgs, tn.BroadcastedMsgs...)
			}
		}
		if bn := r.GetBeaconNode(); bn != nil {
			if bw, ok := bn.(*protocoltesting.BeaconNodeWrapped); ok {
				broadcastedRoots = append(broadcastedRoots, bw.GetBroadcastedRoots()...)
			}
		}
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
				// In committee spectests we bypass queues; treat retryable errors as transient.
				if runner.IsRetryable(err) {
					// ignore and continue; later messages will complete the flow
				} else {
					lastErr = err
				}
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
	fuluFork := phase0.Fork{
		PreviousVersion: beaconCfg.Forks[eth2clientspec.DataVersionFulu].PreviousVersion,
		CurrentVersion:  beaconCfg.Forks[eth2clientspec.DataVersionFulu].CurrentVersion,
		Epoch:           math.MaxUint64, // aggregator committee spec tests are implemented for Electra
	}
	beaconCfg.Forks[eth2clientspec.DataVersionFulu] = fuluFork

	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg

	// Normalize runners/networks and set value checkers for both expected and actual committee runners.
	for i := range committee.Runners {
		cr := committee.Runners[i]
		cr.BaseRunner.NetworkConfig = &netCfg
		cr.ValCheck = protocoltesting.TestingValueChecker{}
		// Ensure controller instances have a value checker
		for _, inst := range cr.BaseRunner.QBFTController.StoredInstances {
			if inst.ValueChecker == nil {
				inst.ValueChecker = protocoltesting.TestingValueChecker{}
			}
		}
		if cr.BaseRunner.State != nil && cr.BaseRunner.State.RunningInstance != nil && cr.BaseRunner.State.RunningInstance.ValueChecker == nil {
			cr.BaseRunner.State.RunningInstance.ValueChecker = protocoltesting.TestingValueChecker{}
		}
	}
	for i := range test.Committee.Runners {
		cr := test.Committee.Runners[i]
		cr.BaseRunner.NetworkConfig = &netCfg
		cr.ValCheck = protocoltesting.TestingValueChecker{}
		for _, inst := range cr.BaseRunner.QBFTController.StoredInstances {
			if inst.ValueChecker == nil {
				inst.ValueChecker = protocoltesting.TestingValueChecker{}
			}
		}
		if cr.BaseRunner.State != nil && cr.BaseRunner.State.RunningInstance != nil && cr.BaseRunner.State.RunningInstance.ValueChecker == nil {
			cr.BaseRunner.State.RunningInstance.ValueChecker = protocoltesting.TestingValueChecker{}
		}
	}

	if needsAggRunners {
		// Normalize existing aggregator runners on both sides without synthesizing new ones.
		for i := range committee.AggregatorRunners {
			ar := committee.AggregatorRunners[i]
			ar.BaseRunner.NetworkConfig = &netCfg
			ar.ValCheck = protocoltesting.TestingValueChecker{}
			for _, inst := range ar.BaseRunner.QBFTController.StoredInstances {
				if inst.ValueChecker == nil {
					inst.ValueChecker = protocoltesting.TestingValueChecker{}
				}
			}
			if ar.BaseRunner.State != nil && ar.BaseRunner.State.RunningInstance != nil && ar.BaseRunner.State.RunningInstance.ValueChecker == nil {
				ar.BaseRunner.State.RunningInstance.ValueChecker = protocoltesting.TestingValueChecker{}
			}
		}
		for i := range test.Committee.AggregatorRunners {
			ar := test.Committee.AggregatorRunners[i]
			ar.BaseRunner.NetworkConfig = &netCfg
			ar.ValCheck = protocoltesting.TestingValueChecker{}
			for _, inst := range ar.BaseRunner.QBFTController.StoredInstances {
				if inst.ValueChecker == nil {
					inst.ValueChecker = protocoltesting.TestingValueChecker{}
				}
			}
			if ar.BaseRunner.State != nil && ar.BaseRunner.State.RunningInstance != nil && ar.BaseRunner.State.RunningInstance.ValueChecker == nil {
				ar.BaseRunner.State.RunningInstance.ValueChecker = protocoltesting.TestingValueChecker{}
			}
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
