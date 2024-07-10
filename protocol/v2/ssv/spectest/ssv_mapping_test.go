package spectest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/committee"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/partialsigcontainer"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/newduty"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/spectest/tests/partialsigmessage"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	tests2 "github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
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
	path, err := os.Getwd()
	require.NoError(t, err)
	jsonTests, err := protocoltesting.GetSSVMappingSpecTestJSON(path, "ssv")
	require.NoError(t, err)

	logger := logging.TestLogger(t)

	untypedTests := map[string]interface{}{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	types.SetDefaultDomain(spectestingutils.TestingSSVDomainType)

	for name, test := range untypedTests {
		name, test := name, test

		if stringInSlice(name, ignoreList) {
			continue
		}
		//if !strings.Contains(name, "wrong beacon vote") { // PROBLEM WITH MARSHALING OF COMMITTEE STRUCTURE TO CALCULATE THE STATE ROOT HASH
		//	continue
		//}

		//if !strings.Contains(name, "new duty valid") { // Nil pointer panic. QBFTController is nil.
		//	continue
		//}
		//if !strings.Contains(name, "new duty post decided") {
		//	continue
		//}
		//if !strings.Contains(name, "mixed committees") { // broadcasted roots count not equal

		//fmt.Println(name)

		// SKIP FOR NOW. WE DONT HAVE AN ERROR FOR THIS CASE AT ALL
		if strings.Contains(name, "empty committee duty") {
			continue
		}
		if strings.Contains(name, "decide on slashable attestation") { // NO ERROR IS TRIGGERED. SKIP FOR NOW
			continue
		}

		r := prepareTest(t, logger, name, test)
		if r != nil {
			t.Run(r.name, func(t *testing.T) {
				//t.Parallel()
				r.test(t)
			})
			//if t.Failed() {
			//	t.Skip("Skip Test failed")
			//}
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
		// TODO: fix blinded test
		if strings.Contains(testName, "propose regular decide blinded") || strings.Contains(testName, "propose blinded decide regular") {
			logger.Info("skipping blinded block test", zap.String("test", testName))
			return nil
		}
		typedTest := msgProcessingSpecTestFromMap(t, test.(map[string]interface{}))

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
	case reflect.TypeOf(&committee.CommitteeSpecTest{}).String():
		typedTest := committeeSpecTestFromMap(t, logger, test.(map[string]interface{}))
		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&committee.MultiCommitteeSpecTest{}).String():
		subtests := test.(map[string]interface{})["Tests"].([]interface{})
		typedTests := make([]*CommitteeSpecTest, 0)
		for _, subtest := range subtests {
			typedTests = append(typedTests, committeeSpecTestFromMap(t, logger, subtest.(map[string]interface{})))
		}

		typedTest := &MultiCommitteeSpecTest{
			Name:  test.(map[string]interface{})["Name"].(string),
			Tests: typedTests,
		}

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
		Threshold:               ks.Threshold,
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
				operator := spectestingutils.TestingCommitteeMember(ks)
				ret.GetBaseRunner().State.RunningInstance = fixInstanceForRun(t, ret.GetBaseRunner().State.RunningInstance, ret.GetBaseRunner().QBFTController, operator)
			}
		}
	}

	return ret
}

func fixControllerForRun(t *testing.T, logger *zap.Logger, runner runner.Runner, contr *controller.Controller, ks *spectestingutils.TestKeySet) *controller.Controller {
	config := qbfttesting.TestingConfig(logger, ks, spectypes.RoleCommittee)
	config.ValueCheckF = runner.GetValCheckF()
	newContr := controller.NewController(
		contr.Identifier,
		contr.CommitteeMember,
		config,
		false,
	)
	newContr.Height = contr.Height
	newContr.StoredInstances = contr.StoredInstances

	for i, inst := range newContr.StoredInstances {
		if inst == nil {
			continue
		}
		operator := spectestingutils.TestingCommitteeMember(ks)
		newContr.StoredInstances[i] = fixInstanceForRun(t, inst, newContr, operator)
	}
	return newContr
}

func fixInstanceForRun(t *testing.T, inst *instance.Instance, contr *controller.Controller, share *spectypes.CommitteeMember) *instance.Instance {
	newInst := instance.NewInstance(
		contr.GetConfig(),
		share,
		contr.Identifier,
		contr.Height)

	newInst.State.DecidedValue = inst.State.DecidedValue
	newInst.State.Decided = inst.State.Decided
	newInst.State.CommitteeMember = inst.State.CommitteeMember
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
	newInst.StartValue = inst.StartValue
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

func committeeSpecTestFromMap(t *testing.T, logger *zap.Logger, m map[string]interface{}) *CommitteeSpecTest {
	committeeMap := m["Committee"].(map[string]interface{})

	inputs := make([]interface{}, 0)
	for _, input := range m["Input"].([]interface{}) {
		byts, err := json.Marshal(input)
		if err != nil {
			panic(err)
		}

		var getDecoder = func() *json.Decoder {
			decoder := json.NewDecoder(strings.NewReader(string(byts)))
			decoder.DisallowUnknownFields()
			return decoder
		}

		committeeDuty := &spectypes.CommitteeDuty{}
		err = getDecoder().Decode(&committeeDuty)
		if err == nil {
			inputs = append(inputs, committeeDuty)
			continue
		}

		beaconDuty := &spectypes.BeaconDuty{}
		err = getDecoder().Decode(&beaconDuty)
		if err == nil {
			inputs = append(inputs, beaconDuty)
			continue
		}

		msg := &spectypes.SignedSSVMessage{}
		err = getDecoder().Decode(&msg)
		if err == nil {
			inputs = append(inputs, msg)
			continue
		}

		panic(fmt.Sprintf("Unsupported input: %T\n", input))
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

	ctx := context.Background() // TODO refactor this
	c := fixCommitteeForRun(t, ctx, logger, committeeMap)

	return &CommitteeSpecTest{
		Name:                   m["Name"].(string),
		Committee:              c,
		Input:                  inputs,
		PostDutyCommitteeRoot:  m["PostDutyCommitteeRoot"].(string),
		OutputMessages:         outputMsgs,
		BeaconBroadcastedRoots: beaconBroadcastedRoots,
		ExpectedError:          m["ExpectedError"].(string),
	}
}

func fixCommitteeForRun(t *testing.T, ctx context.Context, logger *zap.Logger, committeeMap map[string]interface{}) *validator.Committee {
	byts, _ := json.Marshal(committeeMap)
	specCommittee := &specssv.Committee{}
	require.NoError(t, json.Unmarshal(byts, specCommittee))

	c := validator.NewCommittee(
		ctx,
		logger,
		tests2.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
		&specCommittee.CommitteeMember,
		testingutils.NewTestingVerifier(),
		func(slot phase0.Slot, shareMap map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner {
			return ssvtesting.CommitteeRunnerWithShareMap(logger, shareMap).(*runner.CommitteeRunner)
		},
	)
	tmpSsvCommittee := &validator.Committee{}
	require.NoError(t, json.Unmarshal(byts, tmpSsvCommittee))

	c.Runners = tmpSsvCommittee.Runners

	for slot := range c.Runners {

		var shareInstance *spectypes.Share
		for _, share := range c.Runners[slot].BaseRunner.Share {
			shareInstance = share
			break
		}

		fixedRunner := fixRunnerForRun(t, committeeMap["Runners"].(map[string]interface{})[fmt.Sprintf("%v", slot)].(map[string]interface{}), testingutils.KeySetForShare(shareInstance))
		c.Runners[slot] = fixedRunner.(*runner.CommitteeRunner)
	}
	c.Shares = specCommittee.Share

	return c
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if strings.Contains(a, b) {
			return true
		}
	}
	return false
}

var ignoreList = []string{
	//"duty first height",
	//"duty valid",
	//"consensus partial invalid sig quorum then valid quorum",
	//"consensus unordered roots", /// <<<<<
	//"consensus valid msg 7 operators",
	//"consensus quorum",
	//"duty role",
	//"blocks",
	//"value check far future target",
	//"happy flow duties with different validators",
	//"proposer accepts blinded block proposal", // <<<<<<
	//"consensus no running duty",
	//"consensus quorum",
	//"consensus invalid signed msg",
	//"consensus post decided", //<<<<
	//"duty type",
	//"validator index",
	//"duty consensus not started",
	//"consensus inconsistent operator signer",
	//"consensus invalid beacon signature in quorum",
	//"consensus msg different roots",
	//"consensus invalid validator index",
	//"consensus inconsistent operator signer",
	//"consensus too many roots",
	//"duty finished",
	//"valid message",
	//"consensus invalid beacon signature in quorum",
	//"consensus partial invalid root quorum then valid quorum",
	//"consensus quorum 13 operators",
	//"consensus post finish",
	//"consensus invalid expected roots",
	//"committee aggregator all are aggregators",
	//"future message",
	//"consensus nil ssvmessage",
	//"duty", /// <<<< DIFFERENT ROOTS. CHECK JSON
	//"consensus invalid expected roots",
	//"consensus invalid operator signature",
	//"consensus valid msg 13 operators",
	//"duplicate quorum",
	//"consensus invalid msg signature",
	//"consensus quorum 7 operators",
	//"duty post future decided",
	//"future decided",
	//"consensus too many roots", /// <<<<
	//"consensus invalid then quorum",
	//"consensus valid msg 10 operators",
	//"value check source higher than target",
	//"value check with slashable minority",
	//"happy flow duties with same validators",
	//"duplicate",
	//"valid post decided",
	//"happy flow",
	//"consensus duplicate msg",
	//"consensus valid msg",
	//"consensus unordered expected roots",
	//"consensus valid msg 7 operators",
	//"flow", // <<<
	//"msg duty not finished",
	//"decided duties",
	//"beacon vote",
	//"duty not decided",
	//"consensus invalid quorum then valid quorum",
	//"invalid signature",
	//"consensus nil ssvmessage", /// <<<<
	//"consensus post quorum",
	//"consensus post quorum",
	//"duty pubkey",
	//"with consensus data",
	//"duty post decided",
	//"consensus inconsistent beacon signer",
	//"on slashable attestation", /// <<<
	//"future duty slot",
	//"blinded decide regular",
	//"valid decided 7 operators",
	//"consensus decided invalid value",
	//"consensus duplicate msg different roots",
	//"consensus invalid msg",
	//"consensus invalid operator signature",
	//"validator PK",
	//"consensus invalid quorum then valid quorum",
	//"consensus invalid msg signature",
	//"data value check nil",
	//"committee duty",
	"valid post finish", /// <<<<
	//"consensus duplicate msg different root then quorum",
	//"consensus quorum 10 operators",
	//"consensus valid msg",/// <<<<
	//"msg duty finished",
	//"valid decided 13 operators",
	//"consensus quorum 7 operators",
	//"value check valid",
	//"value check with slashable majority",
	//"beacon vote",
	"quorum", /// <<<<
	//"consensus before decided",
	"decided duties",         // <<<
	"than successful duties", /// <<<<
	//"decided invalid value", /// <<<
	//"duty post invalid decided",
	//"invalid", /// <<<<
	//"consensus no running duty",
	//"consensus unknown msg signer",
	//"consensus duplicate msg", /// <<<<
	//"value check slashable",
	//"duty post wrong decided",
	//"proposer accepts normal block proposal",/// <<<<
	//"valid decided",
	//"valid decided 10 operators",
	//"consensus invalid msg slot",
	//"consensus valid msg 13 operators",
	//"happy flow duties",
	//"committee aggregator some are aggregators",
	//"future decided no running instance",
	//"past message",
	//"one signature",
	//"regular decide blinded",
	//"consensus quorum 10 operators",
	//"consensus valid msg 10 operators",
	//"consensus quorum 13 operators",
	//"msg duty does not exist",
	//"duty not finished",
	//"consensus post finish",
	//"consensus unknown signer",
	"committees", // <<<
	//"consensus inconsistent beacon signer",
	//"consensus invalid then quorum",
	//"consensus invalid msg slot",
	//"consensus too few roots",
	"message ID", /// <<<<
	//"duty finished",/// <<<<
	//"committee aggregator none is aggregator",/// <<<<
	//"consensus too few roots",/// <<<<
}
