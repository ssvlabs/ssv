package spectest

import (
	"encoding/json"
	"github.com/ssvlabs/ssv/exporter/convert"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/partialsigcontainer"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/newduty"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/spectest/tests/partialsigmessage"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	qbfttesting "github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestSSVMapping(t *testing.T) {
	path, _ := os.Getwd()
	jsonTests, err := protocoltesting.GetSpecTestJSON(path, "ssv")
	require.NoError(t, err)

	logger := logging.TestLogger(t)

	untypedTests := map[string]interface{}{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	types.SetDefaultDomain(spectestingutils.TestingSSVDomainType)

	for name, test := range untypedTests {
		name, test := name, test
		r := prepareTest(t, logger, name, test)
		if r != nil {
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				r.test(t)
			})
		}
	}
}

type runnable struct {
	name string
	test func(t *testing.T)
}

func prepareTest(t *testing.T, logger *zap.Logger, name string, test interface{}) *runnable {
	testName := strings.Split(name, "_")[1]
	testType := strings.Split(name, "_")[0]

	switch testType {
	case reflect.TypeOf(&tests.MsgProcessingSpecTest{}).String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &MsgProcessingSpecTest{
			Runner: &runner.CommitteeRunner{},
		}
		// TODO: fix blinded test
		if strings.Contains(testName, "propose regular decide blinded") || strings.Contains(testName, "propose blinded decide regular") {
			logger.Info("skipping blinded block test", zap.String("test", testName))
			return nil
		}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			},
		}
	case reflect.TypeOf(&tests.MultiMsgProcessingSpecTest{}).String():
		typedTest := &MultiMsgProcessingSpecTest{
			Name: test.(map[string]interface{})["Name"].(string),
		}
		subtests := test.(map[string]interface{})["Tests"].([]interface{})
		for _, subtest := range subtests {
			typedTest.Tests = append(typedTest.Tests, msgProcessingSpecTestFromMap(t, subtest.(map[string]interface{})))
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&partialsigmessage.MsgSpecTest{}).String(): // no use of internal structs so can run as spec test runs
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &partialsigmessage.MsgSpecTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&valcheck.SpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &valcheck.SpecTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&valcheck.MultiSpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &valcheck.MultiSpecTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest{}).String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				RunSyncCommitteeAggProof(t, typedTest)
			},
		}
	case reflect.TypeOf(&newduty.MultiStartNewRunnerDutySpecTest{}).String():
		typedTest := &MultiStartNewRunnerDutySpecTest{
			Name: test.(map[string]interface{})["Name"].(string),
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				subtests := test.(map[string]interface{})["Tests"].([]interface{})
				for _, subtest := range subtests {
					typedTest.Tests = append(typedTest.Tests, newRunnerDutySpecTestFromMap(t, subtest.(map[string]interface{})))
				}
				typedTest.Run(t, logger)
			},
		}
	case reflect.TypeOf(&partialsigcontainer.PartialSigContainerTest{}).String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &partialsigcontainer.PartialSigContainerTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	default:
		t.Fatalf("unsupported test type %s [%s]", testType, testName)
		return nil
	}
}

func newRunnerDutySpecTestFromMap(t *testing.T, m map[string]interface{}) *StartNewRunnerDutySpecTest {
	runnerMap := m["Runner"].(map[string]interface{})
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	var testDuty spectypes.Duty
	if _, ok := m["CommitteeDuty"]; ok {
		byts, err := json.Marshal(m["CommitteeDuty"])
		if err != nil {
			panic("cant marshal committee duty")
		}
		committeeDuty := &spectypes.CommitteeDuty{}
		err = json.Unmarshal(byts, committeeDuty)
		if err != nil {
			panic("cant unmarshal committee duty")
		}
		testDuty = committeeDuty
	} else if _, ok := m["BeaconDuty"]; ok {
		byts, err := json.Marshal(m["BeaconDuty"])
		if err != nil {
			panic("cant marshal beacon duty")
		}
		beaconDuty := &spectypes.BeaconDuty{}
		err = json.Unmarshal(byts, beaconDuty)
		if err != nil {
			panic("cant unmarshal beacon duty")
		}
		testDuty = beaconDuty
	} else {
		panic("no beacon or committee duty")
	}

	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0)
	for _, msg := range m["OutputMessages"].([]interface{}) {
		byts, _ := json.Marshal(msg)
		typedMsg := &spectypes.PartialSignatureMessages{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		outputMsgs = append(outputMsgs, typedMsg)
	}

	shareInstance := &spectypes.Share{}
	for _, share := range baseRunnerMap["Share"].(map[string]interface{}) {
		shareBytes, err := json.Marshal(share)
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(shareBytes, shareInstance)
		if err != nil {
			panic(err)
		}
	}

	ks := spectestingutils.KeySetForShare(shareInstance)

	r := fixRunnerForRun(t, runnerMap, ks)

	return &StartNewRunnerDutySpecTest{
		Name:                    m["Name"].(string),
		Duty:                    testDuty,
		Runner:                  r,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		ExpectedError:           m["ExpectedError"].(string),
		OutputMessages:          outputMsgs,
	}
}

func msgProcessingSpecTestFromMap(t *testing.T, m map[string]interface{}) *MsgProcessingSpecTest {
	runnerMap := m["Runner"].(map[string]interface{})
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	var duty spectypes.Duty
	if _, ok := m["CommitteeDuty"]; ok {
		byts, err := json.Marshal(m["CommitteeDuty"])
		if err != nil {
			panic("cant marshal committee duty")
		}
		committeeDuty := &spectypes.CommitteeDuty{}
		err = json.Unmarshal(byts, committeeDuty)
		if err != nil {
			panic("cant unmarshal committee duty")
		}
		duty = committeeDuty
	} else if _, ok := m["BeaconDuty"]; ok {
		byts, err := json.Marshal(m["BeaconDuty"])
		if err != nil {
			panic("cant marshal beacon duty")
		}
		beaconDuty := &spectypes.BeaconDuty{}
		err = json.Unmarshal(byts, beaconDuty)
		if err != nil {
			panic("cant unmarshal beacon duty")
		}
		duty = beaconDuty
	} else {
		panic("no beacon or committee duty")
	}

	msgs := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range m["Messages"].([]interface{}) {
		byts, _ := json.Marshal(msg)
		typedMsg := &spectypes.SignedSSVMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		msgs = append(msgs, typedMsg)
	}

	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0)
	require.NotNilf(t, m["OutputMessages"], "OutputMessages can't be nil")
	for _, msg := range m["OutputMessages"].([]interface{}) {
		byts, _ := json.Marshal(msg)
		typedMsg := &spectypes.PartialSignatureMessages{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		outputMsgs = append(outputMsgs, typedMsg)
	}

	beaconBroadcastedRoots := make([]string, 0)
	if m["BeaconBroadcastedRoots"] != nil {
		for _, r := range m["BeaconBroadcastedRoots"].([]interface{}) {
			beaconBroadcastedRoots = append(beaconBroadcastedRoots, r.(string))
		}
	}

	shareInstance := &spectypes.Share{}
	for _, share := range baseRunnerMap["Share"].(map[string]interface{}) {
		shareBytes, err := json.Marshal(share)
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(shareBytes, shareInstance)
		if err != nil {
			panic(err)
		}
	}

	ks := spectestingutils.KeySetForShare(shareInstance)

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

func fixRunnerForRun(t *testing.T, runnerMap map[string]interface{}, ks *spectestingutils.TestKeySet) runner.Runner {
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]interface{})

	base := &runner.BaseRunner{}
	byts, _ := json.Marshal(baseRunnerMap)
	require.NoError(t, json.Unmarshal(byts, &base))

	logger := logging.TestLogger(t)

	ret := baseRunnerForRole(logger, base.RunnerRoleType, base, ks)

	if ret.GetBaseRunner().QBFTController != nil {
		ret.GetBaseRunner().QBFTController = fixControllerForRun(t, logger, ret, ret.GetBaseRunner().QBFTController, ks)
		if ret.GetBaseRunner().State != nil {
			if ret.GetBaseRunner().State.RunningInstance != nil {
				operator := spectestingutils.TestingOperator(ks)
				ret.GetBaseRunner().State.RunningInstance = fixInstanceForRun(t, ret.GetBaseRunner().State.RunningInstance, ret.GetBaseRunner().QBFTController, operator)
			}
		}
	}

	return ret
}

func fixControllerForRun(t *testing.T, logger *zap.Logger, runner runner.Runner, contr *controller.Controller, ks *spectestingutils.TestKeySet) *controller.Controller {
	config := qbfttesting.TestingConfig(logger, ks, convert.RoleCommittee)
	config.ValueCheckF = runner.GetValCheckF()
	newContr := controller.NewController(
		contr.Identifier,
		contr.Share,
		config,
		false,
	)
	newContr.Height = contr.Height
	newContr.StoredInstances = contr.StoredInstances

	for i, inst := range newContr.StoredInstances {
		if inst == nil {
			continue
		}
		operator := spectestingutils.TestingOperator(ks)
		newContr.StoredInstances[i] = fixInstanceForRun(t, inst, newContr, operator)
	}
	return newContr
}

func fixInstanceForRun(t *testing.T, inst *instance.Instance, contr *controller.Controller, share *spectypes.Operator) *instance.Instance {
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

func baseRunnerForRole(logger *zap.Logger, role spectypes.RunnerRole, base *runner.BaseRunner, ks *spectestingutils.TestKeySet) runner.Runner {
	switch role {
	case spectypes.RoleCommittee:
		ret := ssvtesting.CommitteeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	case spectypes.RoleAggregator:
		ret := ssvtesting.AggregatorRunner(logger, ks)
		ret.(*runner.AggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleProposer:
		ret := ssvtesting.ProposerRunner(logger, ks)
		ret.(*runner.ProposerRunner).BaseRunner = base
		return ret
	case spectypes.RoleSyncCommitteeContribution:
		ret := ssvtesting.SyncCommitteeContributionRunner(logger, ks)
		ret.(*runner.SyncCommitteeAggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleValidatorRegistration:
		ret := ssvtesting.ValidatorRegistrationRunner(logger, ks)
		ret.(*runner.ValidatorRegistrationRunner).BaseRunner = base
		return ret
	case spectypes.RoleVoluntaryExit:
		ret := ssvtesting.VoluntaryExitRunner(logger, ks)
		ret.(*runner.VoluntaryExitRunner).BaseRunner = base
		return ret
	case testingutils.UnknownDutyType:
		ret := ssvtesting.UnknownDutyTypeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	default:
		panic("unknown beacon role")
	}
}
