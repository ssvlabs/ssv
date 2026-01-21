package spectest

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specssv "github.com/ssvlabs/ssv-spec/ssv"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/committee"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/partialsigcontainer"
	runnerconstruction "github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/construction"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/newduty"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestSSVMapping(t *testing.T) {
	path, err := os.Getwd()
	require.NoError(t, err)
	jsonTests, err := storage.GenerateSpecTestJSON(path, "ssv")
	require.NoError(t, err)

	logger := log.TestLogger(t)

	untypedTests := map[string]any{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	// Set true if you need to check the post run states of actual and expected committees / runners
	if DebugDumpState {
		_ = os.RemoveAll(dumpDir)
		os.Mkdir(dumpDir, 0755)
	}

	for name, test := range untypedTests {
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

func prepareTest(t *testing.T, logger *zap.Logger, name string, test any) *runnable {
	testName := strings.Split(name, "_")[1]
	testType := strings.Split(name, "_")[0]

	switch testType {
	case reflect.TypeOf(&tests.MsgProcessingSpecTest{}).String():
		typedTest := msgProcessingSpecTestFromMap(t, test.(map[string]any))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			},
		}
	case reflect.TypeOf(&tests.MultiMsgProcessingSpecTest{}).String():
		typedTest := &MultiMsgProcessingSpecTest{
			Name: test.(map[string]any)["Name"].(string),
		}
		subtests := test.(map[string]any)["Tests"].([]any)
		for _, subtest := range subtests {
			typedTest.Tests = append(typedTest.Tests, msgProcessingSpecTestFromMap(t, subtest.(map[string]any)))
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&valcheck.SpecTest{}).String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		specTest := &valcheck.SpecTest{}
		require.NoError(t, json.Unmarshal(byts, &specTest))
		// Wrap with our implementation's value checkers
		typedTest := &ValCheckSpecTest{SpecTest: specTest}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&valcheck.MultiSpecTest{}).String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		specTest := &valcheck.MultiSpecTest{}
		require.NoError(t, json.Unmarshal(byts, &specTest))
		// Wrap with our implementation's value checkers
		tests := make([]*ValCheckSpecTest, len(specTest.Tests))
		for i, t := range specTest.Tests {
			tests[i] = &ValCheckSpecTest{SpecTest: t}
		}
		typedTest := &MultiValCheckSpecTest{Name: specTest.Name, Tests: tests}

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
			Name: test.(map[string]any)["Name"].(string),
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				subtests := test.(map[string]any)["Tests"].([]any)
				for _, subtest := range subtests {
					typedTest.Tests = append(typedTest.Tests, newRunnerDutySpecTestFromMap(t, subtest.(map[string]any)))
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
		typedTest := committeeSpecTestFromMap(t, logger, test.(map[string]any))
		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeOf(&committee.MultiCommitteeSpecTest{}).String():
		subtests := test.(map[string]any)["Tests"].([]any)
		typedTests := make([]*CommitteeSpecTest, 0)
		for _, subtest := range subtests {
			typedTests = append(typedTests, committeeSpecTestFromMap(t, logger, subtest.(map[string]any)))
		}

		typedTest := &MultiCommitteeSpecTest{
			Name:  test.(map[string]any)["Name"].(string),
			Tests: typedTests,
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}

	case reflect.TypeOf(&runnerconstruction.RunnerConstructionSpecTest{}).String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &RunnerConstructionSpecTest{}
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

func newRunnerDutySpecTestFromMap(t *testing.T, m map[string]any) *StartNewRunnerDutySpecTest {
	runnerMap := m["Runner"].(map[string]any)
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

	testDuty, err := decodeDutyFromMap(m)
	if err != nil {
		panic("no beacon or committee duty")
	}

	outputMsgs, err := decodeOutputMessages(m["OutputMessages"])
	require.NoError(t, err)

	shareInstance := &spectypes.Share{}
	for _, share := range baseRunnerMap["Share"].(map[string]any) {
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

	r := fixRunnerForRun(t, runnerMap, ks, networkconfig.TestNetwork)

	return &StartNewRunnerDutySpecTest{
		Name:                    m["Name"].(string),
		Duty:                    testDuty,
		Runner:                  r,
		Threshold:               ks.Threshold,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		ExpectedErrorCode:       int(m["ExpectedErrorCode"].(float64)),
		OutputMessages:          outputMsgs,
	}
}

func msgProcessingSpecTestFromMap(t *testing.T, m map[string]any) *MsgProcessingSpecTest {
	runnerMap := m["Runner"].(map[string]any)
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

	duty, err := decodeDutyFromMap(m)
	if err != nil {
		panic("no beacon or committee duty")
	}

	rawMsgs := m["Messages"].([]any)
	msgs := make([]*spectypes.SignedSSVMessage, 0, len(rawMsgs))
	for _, msg := range rawMsgs {
		byts, err := json.Marshal(msg)
		require.NoError(t, err)
		typedMsg := &spectypes.SignedSSVMessage{}
		require.NoError(t, json.Unmarshal(byts, typedMsg))
		msgs = append(msgs, typedMsg)
	}

	outputMsgs, err := decodeOutputMessages(m["OutputMessages"])
	require.NoError(t, err)

	beaconBroadcastedRoots, err := decodeBeaconRoots(m["BeaconBroadcastedRoots"])
	require.NoError(t, err)

	shareInstance := &spectypes.Share{}
	for _, share := range baseRunnerMap["Share"].(map[string]any) {
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
	r := fixRunnerForRun(t, runnerMap, ks, networkconfig.TestNetwork)

	return &MsgProcessingSpecTest{
		Name:                    m["Name"].(string),
		Duty:                    duty,
		Runner:                  r,
		Messages:                msgs,
		DecidedSlashable:        m["DecidedSlashable"].(bool),
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		DontStartDuty:           m["DontStartDuty"].(bool),
		ExpectedErrorCode:       int(m["ExpectedErrorCode"].(float64)),
		OutputMessages:          outputMsgs,
		BeaconBroadcastedRoots:  beaconBroadcastedRoots,
	}
}

func fixRunnerForRun(
	t *testing.T,
	runnerMap map[string]any,
	ks *spectestingutils.TestKeySet,
	netCfg *networkconfig.Network,
) runner.Runner {
	logger := log.TestLogger(t)

	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

	baseRunner := &runner.BaseRunner{}
	byts, err := json.Marshal(baseRunnerMap)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(byts, &baseRunner))
	if netCfg == nil {
		netCfg = networkconfig.TestNetwork
	}
	baseRunner.NetworkConfig = netCfg

	ret := createRunnerWithBaseRunner(logger, baseRunner.RunnerRoleType, baseRunner, ks)

	if baseRunner.QBFTController != nil {
		baseRunner.QBFTController = fixControllerForRun(logger, baseRunner.QBFTController, ks)
		if baseRunner.State != nil {
			if baseRunner.State.RunningInstance != nil {
				operator := spectestingutils.TestingCommitteeMember(ks)
				baseRunner.State.RunningInstance = fixInstanceForRun(logger, ks, baseRunner.State.RunningInstance, baseRunner.QBFTController, operator)
			}
		}
	}

	return ret
}

func fixControllerForRun(logger *zap.Logger, contr *controller.Controller, ks *spectestingutils.TestKeySet) *controller.Controller {
	config := protocoltesting.TestingConfig(logger, ks)
	newContr := controller.NewController(
		contr.Identifier,
		contr.CommitteeMember,
		config,
		spectestingutils.NewOperatorSigner(ks, 1),
		false,
	)
	newContr.Height = contr.Height
	newContr.StoredInstances = contr.StoredInstances

	for i, inst := range newContr.StoredInstances {
		if inst == nil {
			continue
		}
		operator := spectestingutils.TestingCommitteeMember(ks)
		newContr.StoredInstances[i] = fixInstanceForRun(logger, ks, inst, newContr, operator)
	}
	return newContr
}

func fixInstanceForRun(
	logger *zap.Logger,
	ks *spectestingutils.TestKeySet,
	inst *instance.Instance,
	contr *controller.Controller,
	share *spectypes.CommitteeMember,
) *instance.Instance {
	signer := spectestingutils.NewOperatorSigner(ks, 1)
	newInst := instance.NewInstance(
		logger,
		contr.GetConfig(),
		share,
		contr.Identifier,
		contr.Height,
		signer,
	)

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

func createRunnerWithBaseRunner(logger *zap.Logger, role spectypes.RunnerRole, base *runner.BaseRunner, ks *spectestingutils.TestKeySet) runner.Runner {
	switch role {
	case spectypes.RoleCommittee:
		ret := ssvtesting.CommitteeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	case ssvtypes.RoleAggregator:
		ret := ssvtesting.AggregatorRunner(logger, ks)
		ret.(*runner.AggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleProposer:
		ret := ssvtesting.ProposerRunner(logger, ks)
		ret.(*runner.ProposerRunner).BaseRunner = base
		return ret
	case ssvtypes.RoleSyncCommitteeContribution:
		ret := ssvtesting.SyncCommitteeContributionRunner(logger, ks)
		ret.(*runner.SyncCommitteeAggregatorRunner).BaseRunner = base
		return ret
	case spectypes.RoleValidatorRegistration:
		ret := ssvtesting.ValidatorRegistrationRunner(logger, ks)
		ret.(*runner.ValidatorRegistrationRunner).BaseRunner = base
		return ret
	case spectypes.RoleAggregatorCommittee:
		ret := ssvtesting.AggregatorCommitteeRunner(logger, ks)
		ret.(*runner.AggregatorCommitteeRunner).BaseRunner = base
		return ret
	case spectypes.RoleVoluntaryExit:
		ret := ssvtesting.VoluntaryExitRunner(logger, ks)
		ret.(*runner.VoluntaryExitRunner).BaseRunner = base
		return ret
	case spectestingutils.UnknownDutyType:
		ret := ssvtesting.UnknownDutyTypeRunner(logger, ks)
		ret.(*runner.CommitteeRunner).BaseRunner = base
		return ret
	default:
		panic("unknown beacon role")
	}
}

func committeeSpecTestFromMap(t *testing.T, logger *zap.Logger, m map[string]any) *CommitteeSpecTest {
	committeeMap := m["Committee"].(map[string]any)

	inputs := make([]any, 0)
	needsAggRunners := false
	for _, input := range m["Input"].([]any) {
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
			if len(committeeDuty.ValidatorDuties) > 0 {
				firstDuty := committeeDuty.ValidatorDuties[0]
				if firstDuty.Type == spectypes.BNRoleAggregator || firstDuty.Type == spectypes.BNRoleSyncCommitteeContribution {
					aggregatorCommitteeDuty := &spectypes.AggregatorCommitteeDuty{}
					err = json.Unmarshal(byts, &aggregatorCommitteeDuty)
					if err == nil {
						needsAggRunners = true
						inputs = append(inputs, aggregatorCommitteeDuty)
						continue
					}
				}
			}
			inputs = append(inputs, committeeDuty)
			continue
		}

		beaconDuty := &spectypes.ValidatorDuty{}
		err = getDecoder().Decode(&beaconDuty)
		if err == nil {
			inputs = append(inputs, beaconDuty)
			continue
		}

		msg := &spectypes.SignedSSVMessage{}
		err = getDecoder().Decode(&msg)
		if err == nil {
			if msg.SSVMessage != nil && msg.SSVMessage.MsgID.GetRoleType() == spectypes.RoleAggregatorCommittee {
				needsAggRunners = true
			}
			inputs = append(inputs, msg)
			continue
		}

		panic(fmt.Sprintf("Unsupported input: %T\n", input))
	}

	outputMsgs, err := decodeOutputMessages(m["OutputMessages"])
	require.NoError(t, err)

	beaconBroadcastedRoots, err := decodeBeaconRoots(m["BeaconBroadcastedRoots"])
	require.NoError(t, err)

	c := fixCommitteeForRun(t, logger, committeeMap, needsAggRunners)

	return &CommitteeSpecTest{
		Name:                   m["Name"].(string),
		Committee:              c,
		Input:                  inputs,
		PostDutyCommitteeRoot:  m["PostDutyCommitteeRoot"].(string),
		OutputMessages:         outputMsgs,
		BeaconBroadcastedRoots: beaconBroadcastedRoots,
		ExpectedErrorCode:      int(m["ExpectedErrorCode"].(float64)),
		NeedsAggRunners:        needsAggRunners,
	}
}

func fixCommitteeForRun(
	t *testing.T,
	logger *zap.Logger,
	committeeMap map[string]any,
	needsAggRunners bool,
) *validator.Committee {
	byts, err := json.Marshal(committeeMap)
	require.NoError(t, err)
	specCommittee := &specssv.Committee{}
	require.NoError(t, json.Unmarshal(byts, specCommittee))

	netCfg := testNetworkConfig(needsAggRunners)
	c := validator.NewCommittee(
		logger,
		netCfg,
		&specCommittee.CommitteeMember,
		func(
			duty spectypes.Duty,
			shareMap map[phase0.ValidatorIndex]*spectypes.Share,
			_ []phase0.BLSPubKey,
			_ runner.CommitteeDutyGuard,
		) (runner.Runner, error) {
			setRunnerNetworkConfig := func(r runner.Runner) runner.Runner {
				switch typed := r.(type) {
				case *runner.CommitteeRunner:
					if typed.BaseRunner != nil {
						typed.BaseRunner.NetworkConfig = netCfg
					}
				case *runner.AggregatorCommitteeRunner:
					if typed.BaseRunner != nil {
						typed.BaseRunner.NetworkConfig = netCfg
					}
				}
				return r
			}
			switch duty.(type) {
			case *spectypes.CommitteeDuty:
				r := ssvtesting.CommitteeRunnerWithShareMap(logger, shareMap)
				return setRunnerNetworkConfig(r), nil
			case *spectypes.AggregatorCommitteeDuty:
				r := ssvtesting.AggregatorCommitteeRunnerWithShareMap(logger, shareMap)
				return setRunnerNetworkConfig(r), nil
			default:
				return nil, fmt.Errorf("unknown duty type: %T", duty)
			}
		},
		specCommittee.Share,
		validator.NewCommitteeDutyGuard(),
	)
	tmpSsvCommittee := &validator.Committee{}
	require.NoError(t, json.Unmarshal(byts, tmpSsvCommittee))

	committeeRunnersMap, _ := committeeMap["CommitteeRunners"].(map[string]any)
	aggregatorRunnersMap, _ := committeeMap["AggregatorCommitteeRunners"].(map[string]any)
	ks := keySetFromShares(c.Shares)
	if (committeeRunnersMap != nil || aggregatorRunnersMap != nil) && ks == nil {
		require.Fail(t, "no shares for runner keyset")
	}

	if committeeRunnersMap != nil {
		c.Runners = make(map[phase0.Slot]*runner.CommitteeRunner, len(committeeRunnersMap))
		for slotStr, rawRunner := range committeeRunnersMap {
			runnerMap, ok := rawRunner.(map[string]any)
			require.True(t, ok, "committee runner entry is not a map")

			slot, err := strconv.ParseUint(slotStr, 10, 64)
			require.NoError(t, err)

			fixedRunner := fixRunnerForRun(t, runnerMap, ks, netCfg)
			if cr, ok := fixedRunner.(*runner.CommitteeRunner); ok {
				c.Runners[phase0.Slot(slot)] = cr
			}
		}
	} else {
		c.Runners = tmpSsvCommittee.Runners
	}

	if aggregatorRunnersMap != nil {
		c.AggregatorRunners = make(map[phase0.Slot]*runner.AggregatorCommitteeRunner, len(aggregatorRunnersMap))
		for slotStr, rawRunner := range aggregatorRunnersMap {
			runnerMap, ok := rawRunner.(map[string]any)
			require.True(t, ok, "aggregator committee runner entry is not a map")

			slot, err := strconv.ParseUint(slotStr, 10, 64)
			require.NoError(t, err)

			fixedRunner := fixRunnerForRun(t, runnerMap, ks, netCfg)
			if acr, ok := fixedRunner.(*runner.AggregatorCommitteeRunner); ok {
				c.AggregatorRunners[phase0.Slot(slot)] = acr
			}
		}
	} else {
		c.AggregatorRunners = tmpSsvCommittee.AggregatorRunners
	}

	for _, cr := range c.Runners {
		applyRunnerNetworkConfig(cr, netCfg)
	}
	for _, ar := range c.AggregatorRunners {
		applyRunnerNetworkConfig(ar, netCfg)
	}

	return c
}
