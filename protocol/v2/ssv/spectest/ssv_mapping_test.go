package spectest

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/messages"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/runner/duties/newduty"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	qbfttesting "github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/bloxapp/ssv/protocol/v2/ssv/testing"
	protocoltesting "github.com/bloxapp/ssv/protocol/v2/testing"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("ssv-mapping-test", zapcore.DebugLevel, nil)
}

func TestSSVMapping(t *testing.T) {
	path, _ := os.Getwd()
	jsonTests, err := protocoltesting.GetSpecTestJSON(path, "ssv")
	require.NoError(t, err)

	untypedTests := map[string]interface{}{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	origDomain := types.GetDefaultDomain()
	types.SetDefaultDomain(spectypes.PrimusTestnet)
	defer func() {
		types.SetDefaultDomain(origDomain)
	}()

	for name, test := range untypedTests {
		logex.Reset()
		name, test := name, test

		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]

		fmt.Printf("--------- %s - %s \n", testType, testName)

		switch testType {
		case reflect.TypeOf(&tests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &MsgProcessingSpecTest{
				Runner: &runner.AttesterRunner{},
			}
			// TODO fix blinded test
			if strings.Contains(testName, "propose regular decide blinded") || strings.Contains(testName, "propose blinded decide regular") {
				continue
			}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			})
		case reflect.TypeOf(&tests.MultiMsgProcessingSpecTest{}).String():
			subtests := test.(map[string]interface{})["Tests"].([]interface{})
			typedTests := make([]*MsgProcessingSpecTest, 0)
			for _, subtest := range subtests {
				typedTests = append(typedTests, msgProcessingSpecTestFromMap(t, subtest.(map[string]interface{})))
			}

			typedTest := &MultiMsgProcessingSpecTest{
				Name:  test.(map[string]interface{})["Name"].(string),
				Tests: typedTests,
			}

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&messages.MsgSpecTest{}).String(): // no use of internal structs so can run as spec test runs
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &messages.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&valcheck.SpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &valcheck.SpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&valcheck.MultiSpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &valcheck.MultiSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunSyncCommitteeAggProof(t, typedTest)
			})
		case reflect.TypeOf(&newduty.MultiStartNewRunnerDutySpecTest{}).String():
			subtests := test.(map[string]interface{})["Tests"].([]interface{})
			typedTests := make([]*StartNewRunnerDutySpecTest, 0)
			for _, subtest := range subtests {
				typedTests = append(typedTests, newRunnerDutySpecTestFromMap(t, subtest.(map[string]interface{})))
			}

			typedTest := &MultiStartNewRunnerDutySpecTest{
				Name:  test.(map[string]interface{})["Name"].(string),
				Tests: typedTests,
			}

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		default:
			t.Fatalf("unsupported test type %s [%s]", testType, testName)
		}
	}
}

func newRunnerDutySpecTestFromMap(t *testing.T, m map[string]interface{}) *StartNewRunnerDutySpecTest {
	runnerMap := m["Runner"].(map[string]interface{})
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	duty := &spectypes.Duty{}
	byts, _ := json.Marshal(m["Duty"])
	require.NoError(t, json.Unmarshal(byts, duty))

	outputMsgs := make([]*ssv.SignedPartialSignatureMessage, 0)
	for _, msg := range m["OutputMessages"].([]interface{}) {
		byts, _ = json.Marshal(msg)
		typedMsg := &ssv.SignedPartialSignatureMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		outputMsgs = append(outputMsgs, typedMsg)
	}

	ks := testingutils.KeySetForShare(&spectypes.Share{Quorum: uint64(baseRunnerMap["Share"].(map[string]interface{})["Quorum"].(float64))})

	r := fixRunnerForRun(t, runnerMap, ks)

	return &StartNewRunnerDutySpecTest{
		Name:                    m["Name"].(string),
		Duty:                    duty,
		Runner:                  r,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		ExpectedError:           m["ExpectedError"].(string),
		OutputMessages:          outputMsgs,
	}
}

func msgProcessingSpecTestFromMap(t *testing.T, m map[string]interface{}) *MsgProcessingSpecTest {
	runnerMap := m["Runner"].(map[string]interface{})
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	duty := &spectypes.Duty{}
	byts, _ := json.Marshal(m["Duty"])
	require.NoError(t, json.Unmarshal(byts, duty))

	msgs := make([]*spectypes.SSVMessage, 0)
	for _, msg := range m["Messages"].([]interface{}) {
		byts, _ = json.Marshal(msg)
		typedMsg := &spectypes.SSVMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		msgs = append(msgs, typedMsg)
	}

	outputMsgs := make([]*ssv.SignedPartialSignatureMessage, 0)
	require.NotNilf(t, m["OutputMessages"], "OutputMessages can't be nil")
	for _, msg := range m["OutputMessages"].([]interface{}) {
		byts, _ = json.Marshal(msg)
		typedMsg := &ssv.SignedPartialSignatureMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		outputMsgs = append(outputMsgs, typedMsg)
	}

	beaconBroadcastedRoots := make([]string, 0)
	if m["BeaconBroadcastedRoots"] != nil {
		for _, r := range m["BeaconBroadcastedRoots"].([]interface{}) {
			beaconBroadcastedRoots = append(beaconBroadcastedRoots, r.(string))
		}
	}

	ks := testingutils.KeySetForShare(&spectypes.Share{Quorum: uint64(baseRunnerMap["Share"].(map[string]interface{})["Quorum"].(float64))})

	// runner
	r := fixRunnerForRun(t, runnerMap, ks)

	return &MsgProcessingSpecTest{
		Name:                    m["Name"].(string),
		Duty:                    duty,
		Runner:                  r,
		Messages:                msgs,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		DontStartDuty:           m["DontStartDuty"].(bool),
		ExpectedError:           m["ExpectedError"].(string),
		OutputMessages:          outputMsgs,
		BeaconBroadcastedRoots:  beaconBroadcastedRoots,
	}
}

func fixRunnerForRun(t *testing.T, runnerMap map[string]interface{}, ks *testingutils.TestKeySet) runner.Runner {
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	base := runner.NewBaseRunner(zap.NewNop())
	byts, _ := json.Marshal(baseRunnerMap)
	require.NoError(t, json.Unmarshal(byts, &base))

	ret := baseRunnerForRole(base.BeaconRoleType, base, ks)

	// specific for blinded block
	if blindedBlocks, ok := runnerMap["ProducesBlindedBlocks"]; ok {
		ret.(*runner.ProposerRunner).ProducesBlindedBlocks = blindedBlocks.(bool)
	}

	if ret.GetBaseRunner().QBFTController != nil {
		ret.GetBaseRunner().QBFTController = fixControllerForRun(t, ret, ret.GetBaseRunner().QBFTController, ks)
		if ret.GetBaseRunner().State != nil {
			if ret.GetBaseRunner().State.RunningInstance != nil {
				ret.GetBaseRunner().State.RunningInstance = fixInstanceForRun(t, ret.GetBaseRunner().State.RunningInstance, ret.GetBaseRunner().QBFTController, ret.GetBaseRunner().Share)
			}
		}
	}

	return ret
}

func fixControllerForRun(t *testing.T, runner runner.Runner, contr *controller.Controller, ks *testingutils.TestKeySet) *controller.Controller {
	config := qbfttesting.TestingConfig(ks, spectypes.BNRoleAttester)
	newContr := controller.NewController(
		contr.Identifier,
		contr.Share,
		testingutils.TestingConfig(ks).Domain,
		config,
		false,
	)
	newContr.StoredInstances = make(controller.InstanceContainer, 0, controller.InstanceContainerTestCapacity)
	newContr.Height = contr.Height
	newContr.Domain = contr.Domain
	newContr.StoredInstances = contr.StoredInstances

	for i, inst := range newContr.StoredInstances {
		if inst == nil {
			continue
		}
		newContr.StoredInstances[i] = fixInstanceForRun(t, inst, newContr, runner.GetBaseRunner().Share)
	}
	return newContr
}

func fixInstanceForRun(t *testing.T, inst *instance.Instance, contr *controller.Controller, share *spectypes.Share) *instance.Instance {
	newInst := instance.NewInstance(
		contr.GetConfig(),
		share,
		contr.Identifier,
		contr.Height)

	newInst.State.DecidedValue = inst.State.DecidedValue
	newInst.State.Decided = inst.State.Decided
	newInst.State.Share = inst.State.Share
	newInst.State.Round = inst.State.Round
	newInst.State.Height = inst.State.Height
	newInst.State.ProposalAcceptedForCurrentRound = inst.State.ProposalAcceptedForCurrentRound
	newInst.State.ID = inst.State.ID
	newInst.State.LastPreparedValue = inst.State.LastPreparedValue
	newInst.State.LastPreparedRound = inst.State.LastPreparedRound
	newInst.State.ProposeContainer = inst.State.ProposeContainer
	newInst.State.PrepareContainer = inst.State.PrepareContainer
	newInst.State.CommitContainer = inst.State.CommitContainer
	newInst.State.RoundChangeContainer = inst.State.RoundChangeContainer
	return newInst
}

func baseRunnerForRole(role spectypes.BeaconRole, base *runner.BaseRunner, ks *testingutils.TestKeySet) runner.Runner {
	switch role {
	case spectypes.BNRoleAttester:
		ret := ssvtesting.AttesterRunner(ks)
		ret.(*runner.AttesterRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleAggregator:
		ret := ssvtesting.AggregatorRunner(ks)
		ret.(*runner.AggregatorRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleProposer:
		ret := ssvtesting.ProposerRunner(ks)
		ret.(*runner.ProposerRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleSyncCommittee:
		ret := ssvtesting.SyncCommitteeRunner(ks)
		ret.(*runner.SyncCommitteeRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleSyncCommitteeContribution:
		ret := ssvtesting.SyncCommitteeContributionRunner(ks)
		ret.(*runner.SyncCommitteeAggregatorRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleValidatorRegistration:
		ret := ssvtesting.ValidatorRegistrationRunner(ks)
		ret.(*runner.ValidatorRegistrationRunner).BaseRunner = base
		return ret
	case testingutils.UnknownDutyType:
		ret := ssvtesting.UnknownDutyTypeRunner(ks)
		ret.(*runner.AttesterRunner).BaseRunner = base
		return ret
	default:
		panic("unknown beacon role")
	}
}
