package spectest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	logger := protocoltesting.SpectestLogger(t)
	lastErr := test.runPreTesting(logger)
	spectests.AssertErrorCode(t, test.ExpectedErrorCode, lastErr)

	broadcastedMsgsCap := 0
	broadcastedRootsCap := 0
	for _, runner := range test.Committee.Runners {
		network := runner.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgsCap += len(network.BroadcastedMsgs)
		broadcastedRootsCap += len(beaconNetwork.GetBroadcastedRoots())
	}

	broadcastedMsgs := make([]*spectypes.SignedSSVMessage, 0, broadcastedMsgsCap)
	broadcastedRoots := make([]phase0.Root, 0, broadcastedRootsCap)
	for _, runner := range test.Committee.Runners {
		network := runner.GetNetwork().(*protocoltesting.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}

	// test output message (in asynchronous order)
	spectestingutils.ComparePartialSignatureOutputMessagesInAsynchronousOrder(t, test.OutputMessages, broadcastedMsgs, test.Committee.CommitteeMember.Committee)

	// test beacon broadcasted msgs
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
			_, _, err = test.Committee.StartDuty(context.TODO(), logger, input.(*spectypes.CommitteeDuty))
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
		strType := reflect.TypeFor[*MultiCommitteeSpecTest]().String()
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

	// v1.2.3 ssv-spec renamed types.Committee.Runners -> CommitteeRunners (and added a sibling
	// AggregatorCommitteeRunners map for the merged runner), so struct-tag-based json.Unmarshal
	// above no longer finds our (unrenamed) Committee.Runners field. Read the raw comparison-state
	// JSON directly instead, falling back to the legacy "Runners" key for older fixtures.
	// This test type (CommitteeSpecTest) only ever exercises RoleCommittee duties (there is no
	// AggregatorCommitteeDuty input path here), so there is no AggregatorCommitteeRunners fixture
	// data to read; initialize an empty map so it matches test.Committee's own zero state (an
	// unset nil map here would otherwise diverge in JSON encoding from the non-nil empty map the
	// Committee constructor always produces, and desync the compared post-duty roots).
	committee.Runners = readCommitteeRunnersFromStateComparison(t, specDir, name, testType)
	committee.AggregatorRunners = make(map[phase0.Slot]*runner.AggregatorCommitteeRunner)

	for slot := range committee.Runners {
		committee.Runners[slot].NetworkConfig = networkconfig.TestNetwork
		// Use test runner as signer source since deserialized runner has no signer
		var signerSource runner.Runner
		if testRunner, ok := test.Committee.Runners[slot]; ok {
			signerSource = testRunner
		}
		committee.Runners[slot].ValCheck = createValueChecker(committee.Runners[slot], signerSource)
	}
	for slot := range test.Committee.Runners {
		test.Committee.Runners[slot].ValCheck = createValueChecker(test.Committee.Runners[slot])
	}

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])

	test.PostDutyCommittee = committee
}

// readCommitteeRunnersFromStateComparison reads the "CommitteeRunners" (falling back to the legacy
// "Runners") map out of the raw state-comparison JSON fixture for the given test, since v1.2.3
// ssv-spec renamed that field and our (unrenamed) validator.Committee struct no longer matches it
// via struct-tag-based json.Unmarshal.
func readCommitteeRunnersFromStateComparison(t *testing.T, specDir string, name string, testType string) map[phase0.Slot]*runner.CommitteeRunner {
	basedir := filepath.Join(specDir, "generate")
	scDir := typescomparable.GetSCDir(basedir, testType)
	path := filepath.Join(scDir, fmt.Sprintf("%s.json", name))

	byts, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(byts, &raw))

	rawRunners, _ := raw["CommitteeRunners"].(map[string]any)
	if rawRunners == nil {
		rawRunners, _ = raw["Runners"].(map[string]any)
	}

	runners := make(map[phase0.Slot]*runner.CommitteeRunner, len(rawRunners))
	for slotStr, rawRunner := range rawRunners {
		slot, err := strconv.ParseUint(slotStr, 10, 64)
		require.NoError(t, err)

		runnerBytes, err := json.Marshal(rawRunner)
		require.NoError(t, err)

		cr := &runner.CommitteeRunner{}
		require.NoError(t, json.Unmarshal(runnerBytes, cr))

		runners[phase0.Slot(slot)] = cr
	}

	return runners
}
