package spectest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvprotocoltesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type MsgProcessingSpecTest struct {
	Name                    string
	ParentName              string
	Runner                  runner.Runner
	Duty                    spectypes.Duty
	Messages                []*spectypes.SignedSSVMessage
	DecidedSlashable        bool // DecidedSlashable makes the decided value slashable. Simulates consensus instances running in parallel.
	PostDutyRunnerStateRoot string
	PostDutyRunnerState     spectypes.Root `json:"-"` // Field is ignored by encoding/json
	// OutputMessages compares pre/ post signed partial sigs to output. We exclude consensus msgs as it's tested in consensus
	OutputMessages         []*spectypes.PartialSignatureMessages
	BeaconBroadcastedRoots []string
	DontStartDuty          bool // if set to true will not start a duty for the runner
	ExpectedError          string
}

func (test *MsgProcessingSpecTest) TestName() string {
	return test.Name
}

func (test *MsgProcessingSpecTest) FullName() string {
	return strings.ReplaceAll(test.ParentName+"_"+test.Name, " ", "_")
}

func RunMsgProcessing(t *testing.T, test *MsgProcessingSpecTest) {
	logger := logging.TestLogger(t)
	test.overrideStateComparison(t)
	test.RunAsPartOfMultiTest(t, logger)
}

func (test *MsgProcessingSpecTest) runPreTesting(ctx context.Context, logger *zap.Logger) (*validator.Validator, *validator.Committee, error) {
	var share *spectypes.Share
	ketSetMap := make(map[phase0.ValidatorIndex]*spectestingutils.TestKeySet)
	if len(test.Runner.GetBaseRunner().Share) == 0 {
		panic("No share in base runner for tests")
	}
	for _, validatorShare := range test.Runner.GetBaseRunner().Share {
		share = validatorShare
		break
	}

	for valIdx, validatorShare := range test.Runner.GetBaseRunner().Share {
		ketSetMap[valIdx] = spectestingutils.KeySetForShare(validatorShare)
	}

	var v *validator.Validator
	var c *validator.Committee
	var lastErr error
	switch test.Runner.(type) {
	case *runner.CommitteeRunner:
		guard := validator.NewCommitteeDutyGuard()
		c = baseCommitteeWithRunnerSample(ctx, logger, ketSetMap, test.Runner.(*runner.CommitteeRunner), guard)

		if test.DontStartDuty {
			r := test.Runner.(*runner.CommitteeRunner)
			r.DutyGuard = guard
			c.Runners[test.Duty.DutySlot()] = r

			// Inform the duty guard of the running duty, if any, so that it won't reject it.
			if r.BaseRunner.State != nil && r.BaseRunner.State.StartingDuty != nil {
				duty, ok := r.BaseRunner.State.StartingDuty.(*spectypes.CommitteeDuty)
				if !ok {
					panic("starting duty not found")
				}
				for _, validatorDuty := range duty.ValidatorDuties {
					err := guard.StartDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.Slot)
					if err != nil {
						panic(err)
					}
					err = guard.ValidDuty(validatorDuty.Type, spectypes.ValidatorPK(validatorDuty.PubKey), validatorDuty.Slot)
					if err != nil {
						panic(err)
					}
				}
			}
		} else {
			lastErr = c.StartDuty(ctx, logger, test.Duty.(*spectypes.CommitteeDuty))
		}

		for _, msg := range test.Messages {
			dmsg, err := wrapSignedSSVMessageToDecodedSSVMessage(msg)
			if err != nil {
				lastErr = err
				continue
			}
			err = c.ProcessMessage(ctx, logger, dmsg)
			if err != nil {
				lastErr = err
			}
			if test.DecidedSlashable && IsQBFTProposalMessage(msg) {
				for _, validatorShare := range test.Runner.GetBaseRunner().Share {
					test.Runner.GetSigner().(*ekm.TestingKeyManagerAdapter).AddSlashableSlot(validatorShare.SharePubKey, spectestingutils.TestingDutySlot)
				}
			}
		}

	default:
		v = ssvprotocoltesting.BaseValidator(logger, spectestingutils.KeySetForShare(share))
		v.DutyRunners[test.Runner.GetBaseRunner().RunnerRoleType] = test.Runner
		v.Network = test.Runner.GetNetwork()

		if !test.DontStartDuty {
			lastErr = v.StartDuty(ctx, logger, test.Duty)
		}
		for _, msg := range test.Messages {
			dmsg, err := wrapSignedSSVMessageToDecodedSSVMessage(msg)
			if err != nil {
				lastErr = err
				continue
			}
			err = v.ProcessMessage(ctx, logger, dmsg)
			if err != nil {
				lastErr = err
			}
		}
	}

	return v, c, lastErr
}

func (test *MsgProcessingSpecTest) RunAsPartOfMultiTest(t *testing.T, logger *zap.Logger) {
	ctx := context.Background()
	v, c, lastErr := test.runPreTesting(ctx, logger)
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	network := &spectestingutils.TestingNetwork{}
	var beaconNetwork *tests.TestingBeaconNodeWrapped
	var committee []*spectypes.Operator

	switch test.Runner.(type) {
	case *runner.CommitteeRunner:
		var runnerInstance *runner.CommitteeRunner
		for _, runner := range c.Runners {
			runnerInstance = runner
			break
		}
		network = runnerInstance.GetNetwork().(*spectestingutils.TestingNetwork)
		beaconNetwork = runnerInstance.GetBeaconNode().(*tests.TestingBeaconNodeWrapped)
		committee = c.CommitteeMember.Committee
	default:
		network = v.Network.(*spectestingutils.TestingNetwork)
		committee = v.Operator.Committee
		beaconNetwork = test.Runner.GetBeaconNode().(*tests.TestingBeaconNodeWrapped)
	}

	// test output message
	spectestingutils.ComparePartialSignatureOutputMessages(t, test.OutputMessages, network.BroadcastedMsgs, committee)

	// test beacon broadcasted msgs
	spectestingutils.CompareBroadcastedBeaconMsgs(t, test.BeaconBroadcastedRoots, beaconNetwork.GetBroadcastedRoots())

	// post root
	postRoot, err := test.Runner.GetRoot()
	require.NoError(t, err)

	if test.PostDutyRunnerStateRoot != hex.EncodeToString(postRoot[:]) {
		diff := dumpState(t, test.Name, test.Runner, test.PostDutyRunnerState)
		logger.Error("post runner state not equal", zap.String("state", diff))
	}
}

//func (test *MsgProcessingSpecTest) compareBroadcastedBeaconMsgs(t *testing.T) {
//	broadcastedRoots := test.Runner.GetBeaconNode().(*tests.TestingBeaconNodeWrapped).GetBroadcastedRoots()
//	require.Len(t, broadcastedRoots, len(test.BeaconBroadcastedRoots))
//	for _, r1 := range test.BeaconBroadcastedRoots {
//		found := false
//		for _, r2 := range broadcastedRoots {
//			if r1 == hex.EncodeToString(r2[:]) {
//				found = true
//				break
//			}
//		}
//		require.Truef(t, found, "broadcasted beacon root not found")
//	}
//}

func (test *MsgProcessingSpecTest) overrideStateComparison(t *testing.T) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "tests.", 1)
	overrideStateComparison(t, test, test.Name, testType)
}

func overrideStateComparison(t *testing.T, test *MsgProcessingSpecTest, name string, testType string) {
	var r runner.Runner
	switch test.Runner.(type) {
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

	// override
	test.PostDutyRunnerState = r

	root, err := r.GetRoot()
	require.NoError(t, err)

	test.PostDutyRunnerStateRoot = hex.EncodeToString(root[:])
}

var baseCommitteeWithRunnerSample = func(
	ctx context.Context,
	logger *zap.Logger,
	keySetMap map[phase0.ValidatorIndex]*spectestingutils.TestKeySet,
	runnerSample *runner.CommitteeRunner,
	committeeDutyGuard *validator.CommitteeDutyGuard,
) *validator.Committee {

	var keySetSample *spectestingutils.TestKeySet
	for _, ks := range keySetMap {
		keySetSample = ks
		break
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	for valIdx, ks := range keySetMap {
		shareMap[valIdx] = spectestingutils.TestingShare(ks, valIdx)
	}

	createRunnerF := func(_ phase0.Slot, shareMap map[phase0.ValidatorIndex]*spectypes.Share, _ []phase0.BLSPubKey, _ runner.CommitteeDutyGuard) (*runner.CommitteeRunner, error) {
		r, err := runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			controller.NewController(
				runnerSample.BaseRunner.QBFTController.Identifier,
				runnerSample.BaseRunner.QBFTController.CommitteeMember,
				runnerSample.BaseRunner.QBFTController.GetConfig(),
				spectestingutils.TestingOperatorSigner(keySetSample),
				false,
			),
			runnerSample.GetBeaconNode(),
			runnerSample.GetNetwork(),
			runnerSample.GetSigner(),
			runnerSample.GetOperatorSigner(),
			runnerSample.GetValCheckF(),
			committeeDutyGuard,
			runnerSample.GetDoppelgangerHandler(),
		)
		return r.(*runner.CommitteeRunner), err
	}
	ctx, cancel := context.WithCancel(ctx)

	c := validator.NewCommittee(
		ctx,
		cancel,
		logger,
		runnerSample.GetBaseRunner().BeaconNetwork,
		spectestingutils.TestingCommitteeMember(keySetSample),
		createRunnerF,
		shareMap,
		committeeDutyGuard,
	)

	return c
}

// wrapSignedSSVMessageToDecodedSSVMessage - wraps a SignedSSVMessage to a DecodedSSVMessage to pass the queue.DecodedSSVMessage to ProcessMessage
// In spec it accepts SignedSSVMessage, but in the protocol it accepts DecodedSSVMessage
// Without handling nil case in tests we get a panic in decodeSignedSSVMessage
func wrapSignedSSVMessageToDecodedSSVMessage(msg *spectypes.SignedSSVMessage) (*queue.SSVMessage, error) {
	var dmsg *queue.SSVMessage
	var err error

	if msg.SSVMessage == nil {
		dmsg = &queue.SSVMessage{
			SignedSSVMessage: msg,
			SSVMessage:       &spectypes.SSVMessage{},
		}
		dmsg.MsgType = spectypes.SSVConsensusMsgType
	} else {
		dmsg, err = queue.DecodeSignedSSVMessage(msg)
	}

	return dmsg, err
}

// Create alias without duty
type MsgProcessingSpecTestAlias struct {
	Name   string
	Runner runner.Runner
	// No duty from type types.Duty. Its interface
	Messages                []*spectypes.SignedSSVMessage
	DecidedSlashable        bool
	PostDutyRunnerStateRoot string
	PostDutyRunnerState     spectypes.Root `json:"-"` // Field is ignored by encoding/json
	OutputMessages          []*spectypes.PartialSignatureMessages
	BeaconBroadcastedRoots  []string
	DontStartDuty           bool // if set to true will not start a duty for the runner
	ExpectedError           string
	BeaconDuty              *spectypes.ValidatorDuty `json:"ValidatorDuty,omitempty"`
	CommitteeDuty           *spectypes.CommitteeDuty `json:"CommitteeDuty,omitempty"`
}

func (t *MsgProcessingSpecTest) MarshalJSON() ([]byte, error) {
	alias := &MsgProcessingSpecTestAlias{
		Name:                    t.Name,
		Runner:                  t.Runner,
		Messages:                t.Messages,
		DecidedSlashable:        t.DecidedSlashable,
		PostDutyRunnerStateRoot: t.PostDutyRunnerStateRoot,
		PostDutyRunnerState:     t.PostDutyRunnerState,
		OutputMessages:          t.OutputMessages,
		BeaconBroadcastedRoots:  t.BeaconBroadcastedRoots,
		DontStartDuty:           t.DontStartDuty,
		ExpectedError:           t.ExpectedError,
	}

	if t.Duty != nil {
		if beaconDuty, ok := t.Duty.(*spectypes.ValidatorDuty); ok {
			alias.BeaconDuty = beaconDuty
		} else if committeeDuty, ok := t.Duty.(*spectypes.CommitteeDuty); ok {
			alias.CommitteeDuty = committeeDuty
		} else {
			return nil, errors.New("can't marshal StartNewRunnerDutySpecTest because t.Duty isn't ValidatorDuty or CommitteeDuty")
		}
	}
	byts, err := json.Marshal(alias)

	return byts, err
}

func (t *MsgProcessingSpecTest) UnmarshalJSON(data []byte) error {
	aux := &MsgProcessingSpecTestAlias{}
	aux.Runner = &runner.CommitteeRunner{}
	// Unmarshal the JSON data into the auxiliary struct
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	t.Name = aux.Name
	t.Runner = aux.Runner
	t.DecidedSlashable = aux.DecidedSlashable
	t.Messages = aux.Messages
	t.PostDutyRunnerStateRoot = aux.PostDutyRunnerStateRoot
	t.PostDutyRunnerState = aux.PostDutyRunnerState
	t.OutputMessages = aux.OutputMessages
	t.BeaconBroadcastedRoots = aux.BeaconBroadcastedRoots
	t.DontStartDuty = aux.DontStartDuty
	t.ExpectedError = aux.ExpectedError

	// Determine which type of duty was marshaled
	if aux.BeaconDuty != nil {
		t.Duty = aux.BeaconDuty
	} else if aux.CommitteeDuty != nil {
		t.Duty = aux.CommitteeDuty
	}

	return nil
}

// IsQBFTProposalMessage checks if the message is a QBFT proposal message
func IsQBFTProposalMessage(msg *spectypes.SignedSSVMessage) bool {
	if msg.SSVMessage.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := specqbft.Message{}
		err := qbftMsg.Decode(msg.SSVMessage.Data)
		if err != nil {
			panic("could not decode message")
		}
		return qbftMsg.MsgType == specqbft.ProposalMsgType
	}
	return false
}
