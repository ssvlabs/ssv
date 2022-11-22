package spectest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	specssvtests "github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/messages"
	specnewduty "github.com/bloxapp/ssv-spec/ssv/spectest/tests/runner/duties/newduty"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("ssv-mapping-test", zapcore.DebugLevel, nil)
}

func TestSSVMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	filePath := path + "/" + fileName
	jsonTests, err := os.ReadFile(filePath)
	if err != nil {
		resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/v0.2.7/ssv/spectest/generate/tests.json")
		require.NoError(t, err)

		defer func() {
			require.NoError(t, resp.Body.Close())
		}()

		jsonTests, err = io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filePath, jsonTests, 0644))
	}

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
			typedTest := &specssvtests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			})
		case reflect.TypeOf(&tests.MultiMsgProcessingSpecTest{}).String():
			subtests := test.(map[string]interface{})["Tests"].([]interface{})
			typedTests := make([]*specssvtests.MsgProcessingSpecTest, 0)
			for _, subtest := range subtests {
				typedTests = append(typedTests, msgProcessingSpecTestFromMap(t, subtest.(map[string]interface{})))
			}

			typedTest := &specssvtests.MultiMsgProcessingSpecTest{
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
				typedTest.Run(t)
			})
		case reflect.TypeOf(&specnewduty.MultiStartNewRunnerDutySpecTest{}).String():
			subtests := test.(map[string]interface{})["Tests"].([]interface{})
			typedTests := make([]*specnewduty.StartNewRunnerDutySpecTest, 0)
			for _, subtest := range subtests {
				typedTests = append(typedTests, newRunnerDutySpecTestFromMap(t, subtest.(map[string]interface{})))
			}

			typedTest := &specnewduty.MultiStartNewRunnerDutySpecTest{
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

func msgProcessingSpecTestFromMap(t *testing.T, m map[string]interface{}) *specssvtests.MsgProcessingSpecTest {
	runnerMap := m["Runner"].(map[string]interface{})["BaseRunner"].(map[string]interface{})

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

	outputMsgs := make([]*specssv.SignedPartialSignatureMessage, 0)
	for _, msg := range m["OutputMessages"].([]interface{}) {
		byts, _ = json.Marshal(msg)
		typedMsg := &specssv.SignedPartialSignatureMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		outputMsgs = append(outputMsgs, typedMsg)
	}

	ks := spectestingutils.KeySetForShare(&spectypes.Share{Quorum: uint64(runnerMap["Share"].(map[string]interface{})["Quorum"].(float64))})

	// runner
	runner := fixRunnerForRun(t, runnerMap, ks)

	return &specssvtests.MsgProcessingSpecTest{
		Name:                    m["Name"].(string),
		Duty:                    duty,
		Runner:                  runner,
		Messages:                msgs,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		DontStartDuty:           m["DontStartDuty"].(bool),
		ExpectedError:           m["ExpectedError"].(string),
		OutputMessages:          outputMsgs,
	}
}

func fixRunnerForRun(t *testing.T, baseRunner map[string]interface{}, ks *spectestingutils.TestKeySet) specssv.Runner {
	base := &specssv.BaseRunner{}
	byts, _ := json.Marshal(baseRunner)
	require.NoError(t, json.Unmarshal(byts, &base))

	ret := baseRunnerForRole(base.BeaconRoleType, base, ks)
	ret.GetBaseRunner().QBFTController = fixControllerForRun(t, ret, ret.GetBaseRunner().QBFTController, ks)
	if ret.GetBaseRunner().State != nil {
		if ret.GetBaseRunner().State.RunningInstance != nil {
			ret.GetBaseRunner().State.RunningInstance = fixInstanceForRun(t, ret.GetBaseRunner().State.RunningInstance, ret.GetBaseRunner().QBFTController, ret.GetBaseRunner().Share)
		}
	}
	return ret
}

func baseRunnerForRole(role spectypes.BeaconRole, base *specssv.BaseRunner, ks *spectestingutils.TestKeySet) specssv.Runner {
	switch role {
	case spectypes.BNRoleAttester:
		ret := spectestingutils.AttesterRunner(ks)
		ret.(*specssv.AttesterRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleAggregator:
		ret := spectestingutils.AggregatorRunner(ks)
		ret.(*specssv.AggregatorRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleProposer:
		ret := spectestingutils.ProposerRunner(ks)
		ret.(*specssv.ProposerRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleSyncCommittee:
		ret := spectestingutils.SyncCommitteeRunner(ks)
		ret.(*specssv.SyncCommitteeRunner).BaseRunner = base
		return ret
	case spectypes.BNRoleSyncCommitteeContribution:
		ret := spectestingutils.SyncCommitteeContributionRunner(ks)
		ret.(*specssv.SyncCommitteeAggregatorRunner).BaseRunner = base
		return ret
	case spectestingutils.UnknownDutyType:
		ret := spectestingutils.UnknownDutyTypeRunner(ks)
		ret.(*specssv.AttesterRunner).BaseRunner = base
		return ret
	default:
		panic("unknown beacon role")
	}
}

func fixControllerForRun(t *testing.T, runner specssv.Runner, contr *specqbft.Controller, ks *spectestingutils.TestKeySet) *specqbft.Controller {
	config := spectestingutils.TestingConfig(ks)
	newContr := specqbft.NewController(
		contr.Identifier,
		contr.Share,
		spectestingutils.TestingConfig(ks).Domain,
		config,
	)
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

func fixInstanceForRun(t *testing.T, inst *specqbft.Instance, contr *specqbft.Controller, share *spectypes.Share) *specqbft.Instance {
	newInst := specqbft.NewInstance(
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
	return newInst
}

func newRunnerDutySpecTestFromMap(t *testing.T, m map[string]interface{}) *specnewduty.StartNewRunnerDutySpecTest {
	runnerMap := m["Runner"].(map[string]interface{})["BaseRunner"].(map[string]interface{})

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

	ks := spectestingutils.KeySetForShare(&spectypes.Share{Quorum: uint64(runnerMap["Share"].(map[string]interface{})["Quorum"].(float64))})

	runner := fixRunnerForRun(t, runnerMap, ks)

	return &specnewduty.StartNewRunnerDutySpecTest{
		Name:                    m["Name"].(string),
		Duty:                    duty,
		Runner:                  runner,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		ExpectedError:           m["ExpectedError"].(string),
		OutputMessages:          outputMsgs,
	}
}
