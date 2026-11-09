package spectest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	stests "github.com/ssvlabs/ssv-spec/ssv/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ibft/storage"
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
	NeedsAggRunners        bool
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
	spectests.AssertErrorCode(t, adjustExpectedErrorCode(test.ExpectedErrorCode), adjustActualError(lastErr))

	broadcastedMsgs, broadcastedRoots := collectCommitteeBroadcasts(test.Committee)

	// test output message (in asynchronous order)
	spectestingutils.ComparePartialSignatureOutputMessagesInAsynchronousOrder(t, test.OutputMessages, broadcastedMsgs, test.Committee.CommitteeMember.Committee)

	compareBroadcastedBeaconRoots(t, test.BeaconBroadcastedRoots, broadcastedRoots)

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
				return fmt.Errorf("failed to decode SignedSSVMessage: %w", err)
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
	strType := reflect.TypeFor[*CommitteeSpecTest]().String()
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
	for _, test := range tests.Tests {
		t.Run(test.TestName(), func(t *testing.T) {
			// Each test runs its own committee, so they can run in parallel, which matters for heavy
			// multi tests such as max-validators.
			t.Parallel()
			tests.overrideStateComparison(t, test)
			test.ParentName = tests.Name
			test.RunAsPartOfMultiTest(t)
		})
	}
}

// overrideStateComparison overrides the post state comparison for a test in the multi test
func (tests *MultiCommitteeSpecTest) overrideStateComparison(t *testing.T, test *CommitteeSpecTest) {
	testsName := strings.ReplaceAll(tests.TestName(), " ", "_")
	path := filepath.Join(testsName, test.TestName())
	strType := reflect.TypeFor[*MultiCommitteeSpecTest]().String()
	strType = strings.Replace(strType, "spectest.", "committee.", 1)
	overrideStateComparisonCommitteeSpecTest(t, test, path, strType)
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

// committeeStateComparison is the spec's committee state-comparison JSON (see the spec's
// Committee.MarshalJSON), decoded only as far as overrideStateComparisonCommitteeSpecTest uses it.
type committeeStateComparison struct {
	// The current spec keys its runners by role, in a layout rebuilt through fixBaseRunnerForRun.
	CommitteeRunners           map[phase0.Slot]*stateComparisonRunner
	AggregatorCommitteeRunners map[phase0.Slot]*stateComparisonRunner
	// The Alan spec has committee runners only, under Runners, in a layout the node's runner decodes as is.
	Runners         map[phase0.Slot]*runner.CommitteeRunner
	CommitteeMember spectypes.CommitteeMember
	Share           map[phase0.ValidatorIndex]*spectypes.Share
}

// stateComparisonRunner is a runner in the spec's committee state-comparison JSON; only its BaseRunner
// carries over to the node's runner (see fixBaseRunnerForRun).
type stateComparisonRunner struct {
	BaseRunner *runner.BaseRunner
}

func overrideStateComparisonCommitteeSpecTest(t *testing.T, test *CommitteeSpecTest, name string, testType string) {
	// Decode the state-comparison JSON once, straight into the types used below: for the max-validators
	// vectors it runs to ~17MB, and each extra pass over it takes seconds under -race.
	byteValue, err := storage.ReadStateComparisonFile("ssv", name, testType)
	require.NoError(t, err)
	state := &committeeStateComparison{}
	require.NoError(t, json.Unmarshal(byteValue, state))

	committee := &validator.Committee{
		Runners:         state.Runners,
		Shares:          state.Share,
		CommitteeMember: &state.CommitteeMember,
	}

	netCfg := testNetworkConfig(test.NeedsAggRunners)

	if state.CommitteeRunners != nil || state.AggregatorCommitteeRunners != nil {
		ks := keySetFromShares(committee.Shares)
		require.NotNil(t, ks, "no shares for runner keyset")

		if state.CommitteeRunners != nil {
			if committee.Runners == nil {
				committee.Runners = make(map[phase0.Slot]*runner.CommitteeRunner, len(state.CommitteeRunners))
			}
			for slot, r := range state.CommitteeRunners {
				require.True(t, r != nil && r.BaseRunner != nil, "committee runner entry has no BaseRunner")

				fixedRunner := fixBaseRunnerForRun(t, r.BaseRunner, ks, netCfg)
				if cr, ok := fixedRunner.(*runner.CommitteeRunner); ok {
					committee.Runners[slot] = cr
				}
			}
		}

		if state.AggregatorCommitteeRunners != nil {
			committee.AggregatorRunners = make(map[phase0.Slot]*runner.AggregatorCommitteeRunner, len(state.AggregatorCommitteeRunners))
			for slot, r := range state.AggregatorCommitteeRunners {
				require.True(t, r != nil && r.BaseRunner != nil, "aggregator committee runner entry has no BaseRunner")

				fixedRunner := fixBaseRunnerForRun(t, r.BaseRunner, ks, netCfg)
				if acr, ok := fixedRunner.(*runner.AggregatorCommitteeRunner); ok {
					committee.AggregatorRunners[slot] = acr
				}
			}
		}
	}

	for slot, cr := range committee.Runners {
		if cr == nil || cr.BaseRunner == nil {
			continue
		}
		applyRunnerNetworkConfig(cr, netCfg)
		var signerSource runner.Runner
		if testRunner, ok := test.Committee.Runners[slot]; ok {
			signerSource = testRunner
		}
		setRunnerValCheck(cr, createValueChecker(cr, signerSource))
	}
	for _, ar := range committee.AggregatorRunners {
		if ar == nil || ar.BaseRunner == nil {
			continue
		}
		applyRunnerNetworkConfig(ar, netCfg)
		ar.ValCheck = createValueChecker(ar)
	}
	for _, cr := range test.Committee.Runners {
		if cr == nil {
			continue
		}
		applyRunnerNetworkConfig(cr, netCfg)
		setRunnerValCheck(cr, createValueChecker(cr))
	}
	for _, ar := range test.Committee.AggregatorRunners {
		if ar == nil {
			continue
		}
		applyRunnerNetworkConfig(ar, netCfg)
		ar.ValCheck = createValueChecker(ar)
	}

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])

	test.PostDutyCommittee = committee
}

// compareBroadcastedBeaconRoots asserts, in linear time, what spectestingutils.CompareBroadcastedBeaconMsgs
// does: the broadcasted roots match the expected ones one-to-one, in any order. The spec helper is quadratic,
// which takes seconds under -race for vectors that broadcast thousands of roots.
func compareBroadcastedBeaconRoots(t *testing.T, expectedRoots []string, broadcastedRoots []phase0.Root) {
	require.Len(t, broadcastedRoots, len(expectedRoots))

	unmatched := make(map[string]int, len(broadcastedRoots))
	for _, r := range broadcastedRoots {
		unmatched[hex.EncodeToString(r[:])]++
	}
	for _, r := range expectedRoots {
		require.Positivef(t, unmatched[r], "broadcasted beacon root not found")
		unmatched[r]--
	}
}

func collectCommitteeBroadcasts(committee *validator.Committee) ([]*spectypes.SignedSSVMessage, []phase0.Root) {
	broadcastedMsgsCap := 0
	broadcastedRootsCap := 0

	for _, runner := range committee.Runners {
		network := runner.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgsCap += len(network.BroadcastedMsgs)
		broadcastedRootsCap += len(beaconNetwork.GetBroadcastedRoots())
	}
	for _, runner := range committee.AggregatorRunners {
		network := runner.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgsCap += len(network.BroadcastedMsgs)
		broadcastedRootsCap += len(beaconNetwork.GetBroadcastedRoots())
	}

	broadcastedMsgs := make([]*spectypes.SignedSSVMessage, 0, broadcastedMsgsCap)
	broadcastedRoots := make([]phase0.Root, 0, broadcastedRootsCap)
	for _, r := range committee.Runners {
		network := r.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}
	for _, r := range committee.AggregatorRunners {
		network := r.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}

	return broadcastedMsgs, broadcastedRoots
}
