package spectest

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

type CommitteeSpecTest struct {
	Name                   string
	ParentName             string
	Committee              *validator.Committee
	Input                  []interface{} // Can be a types.Duty or a *types.SignedSSVMessage
	PostDutyCommitteeRoot  string
	PostDutyCommittee      spectypes.Root `json:"-"` // Field is ignored by encoding/json
	OutputMessages         []*spectypes.PartialSignatureMessages
	BeaconBroadcastedRoots []string
	ExpectedError          string
}

func (test *CommitteeSpecTest) TestName() string {
	return test.Name
}

func (test *CommitteeSpecTest) FullName() string {
	return strings.ReplaceAll(test.ParentName+"_"+test.Name, " ", "_")
}

// RunAsPartOfMultiTest runs the test as part of a MultiCommitteeSpecTest
func (test *CommitteeSpecTest) RunAsPartOfMultiTest(t *testing.T) {
	logger := logging.TestLogger(t)
	lastErr := test.runPreTesting(logger)
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	broadcastedMsgs := make([]*spectypes.SignedSSVMessage, 0)
	broadcastedRoots := make([]phase0.Root, 0)
	for _, runner := range test.Committee.Runners {
		network := runner.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork := runner.GetBeaconNode().(*tests.TestingBeaconNodeWrapped)
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
			err = test.Committee.StartDuty(context.TODO(), logger, input.(*spectypes.CommitteeDuty))
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

func (test *CommitteeSpecTest) GetPostState(logger *zap.Logger) (interface{}, error) {
	lastErr := test.runPreTesting(logger)
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

func (tests *MultiCommitteeSpecTest) GetPostState(logger *zap.Logger) (interface{}, error) {
	ret := make(map[string]spectypes.Root, len(tests.Tests))
	for _, test := range tests.Tests {
		err := test.runPreTesting(logger)
		if err != nil && test.ExpectedError != err.Error() {
			return nil, err
		}
		ret[test.Name] = test.Committee
	}
	return ret, nil
}

func overrideStateComparisonCommitteeSpecTest(t *testing.T, test *CommitteeSpecTest, name string, testType string) {
	specCommittee := &ssv.Committee{}
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	specCommittee, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, specCommittee)

	require.NoError(t, err)
	committee := &validator.Committee{}
	committee, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, committee)
	require.NoError(t, err)

	committee.Shares = specCommittee.Share
	committee.CommitteeMember = &specCommittee.CommitteeMember
	//for _, r := range committee.Runners {
	//	r.BaseRunner.BeaconNetwork = spectypes.BeaconTestNetwork
	//}

	root, err := committee.GetRoot()
	require.NoError(t, err)

	test.PostDutyCommitteeRoot = hex.EncodeToString(root[:])

	test.PostDutyCommittee = committee
}
