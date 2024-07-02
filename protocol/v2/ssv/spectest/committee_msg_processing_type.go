package spectest

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/google/go-cmp/cmp"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv-spec/ssv"
	"github.com/ssvlabs/ssv-spec/types"
)

type CommitteeSpecTest struct {
	Name                   string
	Committee              *validator.Committee
	Input                  []interface{} // Can be a types.Duty or a *types.SignedSSVMessage
	PostDutyCommitteeRoot  string
	PostDutyCommittee      spectypes.Root `json:"-"` // Field is ignored by encoding/json
	OutputMessages         []*types.PartialSignatureMessages
	BeaconBroadcastedRoots []string
	ExpectedError          string
}

func (test *CommitteeSpecTest) TestName() string {
	return test.Name
}

// RunAsPartOfMultiTest runs the test as part of a MultiCommitteeSpecTest
func (test *CommitteeSpecTest) RunAsPartOfMultiTest(t *testing.T) {
	lastErr := test.runPreTesting()

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	broadcastedMsgs := make([]*types.SignedSSVMessage, 0)
	broadcastedRoots := make([]phase0.Root, 0)
	for _, runner := range test.Committee.Runners {
		network := runner.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*tests.TestingBeaconNodeWrapped)
		broadcastedMsgs = append(broadcastedMsgs, network.BroadcastedMsgs...)
		broadcastedRoots = append(broadcastedRoots, beaconNetwork.GetBroadcastedRoots()...)
	}

	// test output message (in asynchronous order)
	spectestingutils.ComparePartialSignatureOutputMessagesInAsynchronousOrder(t, test.OutputMessages, broadcastedMsgs, test.Committee.Operator.Committee)

	// test beacon broadcasted msgs
	spectestingutils.CompareBroadcastedBeaconMsgs(t, test.BeaconBroadcastedRoots, broadcastedRoots)

	// post root
	postRoot, err := test.Committee.GetRoot()
	require.NoError(t, err)

	if test.PostDutyCommitteeRoot != hex.EncodeToString(postRoot[:]) {
		t.Errorf("post runner state not equal: %v", cmp.Diff(test.Committee, test.PostDutyCommittee, cmp.Exporter(func(p reflect.Type) bool { return true })))
	}
}

// Run as an individual test
func (test *CommitteeSpecTest) Run(t *testing.T) {
	test.overrideStateComparison(t)
	test.RunAsPartOfMultiTest(t)
}

func (test *CommitteeSpecTest) runPreTesting() error {

	var lastErr error

	for _, input := range test.Input {

		var err error
		switch input := input.(type) {
		case spectypes.Duty:
			err = test.Committee.StartDuty(nil, input.(*spectypes.CommitteeDuty))
		case *spectypes.SignedSSVMessage:
			err = test.Committee.ProcessMessage(nil, &queue.DecodedSSVMessage{SignedSSVMessage: input})
		default:
			panic("input is neither duty or SignedSSVMessage")
		}
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (test *CommitteeSpecTest) overrideStateComparison(t *testing.T) {
	overrideStateComparisonCommitteeTest(t, test, test.Name, reflect.TypeOf(test).String())
}

func overrideStateComparisonCommitteeTest(t *testing.T, test *CommitteeSpecTest, name string, testType string) {
	committee := &ssv.Committee{}
	basedir, err := os.Getwd()
	require.NoError(t, err)
	committee, err = typescomparable.UnmarshalStateComparison(basedir, name, testType, committee)
	require.NoError(t, err)

	// override
	test.PostDutyCommittee = committee

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])
}

func (test *CommitteeSpecTest) GetPostState() (interface{}, error) {
	lastErr := test.runPreTesting()
	if lastErr != nil && len(test.ExpectedError) == 0 {
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
			test.RunAsPartOfMultiTest(t)
		})
	}
}

// overrideStateComparison overrides the post state comparison for all tests in the multi test
func (tests *MultiCommitteeSpecTest) overrideStateComparison(t *testing.T) {
	testsName := strings.ReplaceAll(tests.TestName(), " ", "_")
	for _, test := range tests.Tests {
		path := filepath.Join(testsName, test.TestName())
		overrideStateComparisonCommitteeSpecTest(t, test, path, reflect.TypeOf(tests).String())
	}
}

func (tests *MultiCommitteeSpecTest) GetPostState() (interface{}, error) {
	ret := make(map[string]types.Root, len(tests.Tests))
	for _, test := range tests.Tests {
		err := test.runPreTesting()
		if err != nil && test.ExpectedError != err.Error() {
			return nil, err
		}
		ret[test.Name] = test.Committee
	}
	return ret, nil
}

func overrideStateComparisonCommitteeSpecTest(t *testing.T, test *CommitteeSpecTest, name string, testType string) {
	committee := &ssv.Committee{}
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("ssv", "spectest"))

	require.NoError(t, err)
	committee, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, committee)
	require.NoError(t, err)

	// override
	test.PostDutyCommittee = committee

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])
}
