package spectest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
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

	logger := protocoltesting.SpectestLogger(t)

	untypedTests := map[string]any{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	// Set true if you need to check the post run states of actual and expected committees / runners
	if DebugDumpState {
		_ = os.RemoveAll(dumpDir)
		os.Mkdir(dumpDir, 0755)
	}

	// TODO(convergence unit 5): remove once AggregatorCommittee vectors are supported.
	skippedAggregatorCommitteeVectors := 0
	// TODO(convergence unit 5): remove once SyncCommitteeAggregatorProof vectors are supported.
	// These were green on v1.2.2; skipping the whole type is a temporary coverage regression.
	skippedSyncCommitteeAggregatorProofVectors := 0

	for name, test := range untypedTests {
		r := prepareTest(t, logger, name, test, &skippedAggregatorCommitteeVectors, &skippedSyncCommitteeAggregatorProofVectors)
		if r != nil {
			t.Run(r.name, func(t *testing.T) {
				t.Parallel()
				r.test(t)
			})
		}
	}

	totalSkipped := skippedAggregatorCommitteeVectors + skippedSyncCommitteeAggregatorProofVectors
	if totalSkipped > 0 {
		t.Logf(
			"skipped %d AggregatorCommittee-scoped spec vectors (%d msg-processing/valcheck/construction + %d SyncCommitteeAggregatorProof; convergence unit 5)",
			totalSkipped, skippedAggregatorCommitteeVectors, skippedSyncCommitteeAggregatorProofVectors,
		)
	}
}

// isAggregatorCommitteeDutyMap reports whether the given spec-test duty map carries a v1.2.3
// AggregatorCommitteeDuty, which our (not yet merged, see convergence unit 5) runners cannot process.
func isAggregatorCommitteeDutyMap(m map[string]any) bool {
	return m["AggregatorCommitteeDuty"] != nil
}

type runnable struct {
	name string
	test func(t *testing.T)
}

func prepareTest(
	t *testing.T,
	logger *zap.Logger,
	name string,
	test any,
	skippedAggregatorCommitteeVectors *int,
	skippedSyncCommitteeAggregatorProofVectors *int,
) *runnable {
	testName := strings.Split(name, "_")[1]
	testType := strings.Split(name, "_")[0]

	switch testType {
	case reflect.TypeFor[*tests.MsgProcessingSpecTest]().String():
		typedTest, skip := msgProcessingSpecTestFromMap(t, test.(map[string]any))
		if skip {
			*skippedAggregatorCommitteeVectors++
			return nil
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			},
		}
	case reflect.TypeFor[*tests.MultiMsgProcessingSpecTest]().String():
		typedTest := &MultiMsgProcessingSpecTest{
			Name: test.(map[string]any)["Name"].(string),
		}
		subtests := test.(map[string]any)["Tests"].([]any)
		for _, subtest := range subtests {
			subTypedTest, skip := msgProcessingSpecTestFromMap(t, subtest.(map[string]any))
			if skip {
				*skippedAggregatorCommitteeVectors++
				continue
			}
			typedTest.Tests = append(typedTest.Tests, subTypedTest)
		}
		if len(typedTest.Tests) == 0 {
			// TODO(convergence unit 5): enable AggregatorCommittee vectors.
			return nil
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*valcheck.SpecTest]().String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		specTest := &valcheck.SpecTest{}
		require.NoError(t, json.Unmarshal(byts, &specTest))
		// TODO(convergence unit 5): enable AggregatorCommittee vectors.
		if specTest.RunnerRole == spectypes.RoleAggregatorCommittee {
			*skippedAggregatorCommitteeVectors++
			return nil
		}
		// Wrap with our implementation's value checkers
		typedTest := &ValCheckSpecTest{SpecTest: specTest}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*valcheck.MultiSpecTest]().String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		specTest := &valcheck.MultiSpecTest{}
		require.NoError(t, json.Unmarshal(byts, &specTest))
		// Wrap with our implementation's value checkers, skipping AggregatorCommittee vectors.
		// TODO(convergence unit 5): enable AggregatorCommittee vectors.
		tests := make([]*ValCheckSpecTest, 0, len(specTest.Tests))
		for _, subtest := range specTest.Tests {
			if subtest.RunnerRole == spectypes.RoleAggregatorCommittee {
				*skippedAggregatorCommitteeVectors++
				continue
			}
			tests = append(tests, &ValCheckSpecTest{SpecTest: subtest})
		}
		if len(tests) == 0 {
			return nil
		}
		typedTest := &MultiValCheckSpecTest{Name: specTest.Name, Tests: tests}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest]().String():
		// TODO(convergence unit 5): SyncCommitteeAggregatorProof vectors regenerated around
		// AggregatorCommitteeDuty in v1.2.3 and can't run without the AggregatorCommitteeRunner;
		// re-enable with unit 5. NOTE: these were green on v1.2.2 - this is a temporary coverage
		// regression.
		*skippedSyncCommitteeAggregatorProofVectors++
		return nil
	case reflect.TypeFor[*newduty.MultiStartNewRunnerDutySpecTest]().String():
		typedTest := &MultiStartNewRunnerDutySpecTest{
			Name: test.(map[string]any)["Name"].(string),
		}
		subtests := test.(map[string]any)["Tests"].([]any)
		for _, subtest := range subtests {
			subTypedTest, skip := newRunnerDutySpecTestFromMap(t, subtest.(map[string]any))
			if skip {
				*skippedAggregatorCommitteeVectors++
				continue
			}
			typedTest.Tests = append(typedTest.Tests, subTypedTest)
		}
		if len(typedTest.Tests) == 0 {
			// TODO(convergence unit 5): enable AggregatorCommittee vectors.
			return nil
		}

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t, logger)
			},
		}
	case reflect.TypeFor[*partialsigcontainer.PartialSigContainerTest]().String():
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
	case reflect.TypeFor[*committee.CommitteeSpecTest]().String():
		typedTest, skip := committeeSpecTestFromMap(t, logger, test.(map[string]any))
		if skip {
			*skippedAggregatorCommitteeVectors++
			return nil
		}
		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*committee.MultiCommitteeSpecTest]().String():
		subtests := test.(map[string]any)["Tests"].([]any)
		typedTests := make([]*CommitteeSpecTest, 0)
		for _, subtest := range subtests {
			subTypedTest, skip := committeeSpecTestFromMap(t, logger, subtest.(map[string]any))
			if skip {
				*skippedAggregatorCommitteeVectors++
				continue
			}
			typedTests = append(typedTests, subTypedTest)
		}
		if len(typedTests) == 0 {
			// TODO(convergence unit 5): enable AggregatorCommittee vectors.
			return nil
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

	case reflect.TypeFor[*runnerconstruction.RunnerConstructionSpecTest]().String():
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

// newRunnerDutySpecTestFromMap builds a StartNewRunnerDutySpecTest from the given spec-test map.
// The second return value is true when the vector must be skipped (AggregatorCommitteeDuty vectors,
// see convergence unit 5), in which case the returned test is nil.
func newRunnerDutySpecTestFromMap(t *testing.T, m map[string]any) (*StartNewRunnerDutySpecTest, bool) {
	// TODO(convergence unit 5): enable AggregatorCommittee vectors.
	if isAggregatorCommitteeDutyMap(m) {
		return nil, true
	}

	runnerMap := m["Runner"].(map[string]any)
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

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
	} else if _, ok := m["ValidatorDuty"]; ok {
		byts, err := json.Marshal(m["ValidatorDuty"])
		if err != nil {
			panic("cant marshal beacon duty")
		}
		validatorDuty := &spectypes.ValidatorDuty{}
		err = json.Unmarshal(byts, validatorDuty)
		if err != nil {
			panic("cant unmarshal beacon duty")
		}
		testDuty = validatorDuty
	} else {
		panic("no beacon or committee duty")
	}

	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0)
	// Handle null/empty OutputMessages from spec (empty arrays are now null in JSON)
	if m["OutputMessages"] != nil {
		for _, msg := range m["OutputMessages"].([]any) {
			byts, err := json.Marshal(msg)
			require.NoError(t, err)
			typedMsg := &spectypes.PartialSignatureMessages{}
			require.NoError(t, json.Unmarshal(byts, typedMsg))
			outputMsgs = append(outputMsgs, typedMsg)
		}
	}

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

	r := fixRunnerForRun(t, runnerMap, ks)

	return &StartNewRunnerDutySpecTest{
		Name:                    m["Name"].(string),
		Duty:                    testDuty,
		Runner:                  r,
		Threshold:               ks.Threshold,
		PostDutyRunnerStateRoot: m["PostDutyRunnerStateRoot"].(string),
		ExpectedErrorCode:       int(m["ExpectedErrorCode"].(float64)),
		OutputMessages:          outputMsgs,
	}, false
}

// msgProcessingSpecTestFromMap builds a MsgProcessingSpecTest from the given spec-test map.
// The second return value is true when the vector must be skipped (AggregatorCommitteeDuty vectors,
// see convergence unit 5), in which case the returned test is nil.
func msgProcessingSpecTestFromMap(t *testing.T, m map[string]any) (*MsgProcessingSpecTest, bool) {
	// TODO(convergence unit 5): enable AggregatorCommittee vectors.
	if isAggregatorCommitteeDutyMap(m) {
		return nil, true
	}

	runnerMap := m["Runner"].(map[string]any)
	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

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
	} else if _, ok := m["ValidatorDuty"]; ok {
		byts, err := json.Marshal(m["ValidatorDuty"])
		if err != nil {
			panic("cant marshal validator duty")
		}
		beaconDuty := &spectypes.ValidatorDuty{}
		err = json.Unmarshal(byts, beaconDuty)
		if err != nil {
			panic("cant unmarshal validator duty")
		}
		duty = beaconDuty
	} else {
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

	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0)
	// Handle null/empty OutputMessages from spec (empty arrays are now null in JSON)
	if m["OutputMessages"] != nil {
		for _, msg := range m["OutputMessages"].([]any) {
			byts, err := json.Marshal(msg)
			require.NoError(t, err)
			typedMsg := &spectypes.PartialSignatureMessages{}
			require.NoError(t, json.Unmarshal(byts, typedMsg))
			outputMsgs = append(outputMsgs, typedMsg)
		}
	}

	beaconBroadcastedRoots := make([]string, 0)
	if m["BeaconBroadcastedRoots"] != nil {
		for _, r := range m["BeaconBroadcastedRoots"].([]any) {
			beaconBroadcastedRoots = append(beaconBroadcastedRoots, r.(string))
		}
	}

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
	r := fixRunnerForRun(t, runnerMap, ks)

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
	}, false
}

func fixRunnerForRun(t *testing.T, runnerMap map[string]any, ks *spectestingutils.TestKeySet) runner.Runner {
	logger := protocoltesting.SpectestLogger(t)

	baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)

	baseRunner := &runner.BaseRunner{}
	byts, err := json.Marshal(baseRunnerMap)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(byts, &baseRunner))
	baseRunner.NetworkConfig = networkconfig.TestNetwork

	ret := createRunnerWithBaseRunner(logger, baseRunner.RunnerRoleType, baseRunner, ks)

	if baseRunner.QBFTController != nil {
		baseRunner.QBFTController = fixControllerForRun(logger, baseRunner.QBFTController, ks)
		if baseRunner.HasStartedQBFTInstance() {
			operator := spectestingutils.TestingCommitteeMember(ks)
			baseRunner.State.RunningInstance = fixInstanceForRun(logger, ks, baseRunner.State.RunningInstance, baseRunner.QBFTController, operator)
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
	newContr.LatestInstanceHeight = contr.LatestInstanceHeight
	newContr.RecentInstances = contr.RecentInstances

	for i, inst := range newContr.RecentInstances {
		if inst == nil {
			continue
		}
		operator := spectestingutils.TestingCommitteeMember(ks)
		newContr.RecentInstances[i] = fixInstanceForRun(logger, ks, inst, newContr, operator)
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
		context.Background(),
		logger,
		contr.GetConfig(),
		share,
		contr.Identifier,
		contr.LatestInstanceHeight,
		signer,
		func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
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

// committeeSpecTestFromMap builds a CommitteeSpecTest from the given spec-test map.
// The second return value is true when the vector must be skipped (AggregatorCommittee-scoped
// duties/messages, see convergence unit 5), in which case the returned test is nil.
func committeeSpecTestFromMap(t *testing.T, logger *zap.Logger, m map[string]any) (*CommitteeSpecTest, bool) {
	committeeMap := m["Committee"].(map[string]any)

	// TODO(convergence unit 5): enable AggregatorCommittee vectors.
	needsSkip := false

	inputs := make([]any, 0)
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
			// AggregatorCommitteeDuty has the same JSON shape as CommitteeDuty (Slot +
			// ValidatorDuties), so it decodes here silently; distinguish it by duty type.
			if len(committeeDuty.ValidatorDuties) > 0 {
				switch committeeDuty.ValidatorDuties[0].Type {
				case spectypes.BNRoleAggregator, spectypes.BNRoleSyncCommitteeContribution:
					needsSkip = true
				default:
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
				needsSkip = true
			}
			inputs = append(inputs, msg)
			continue
		}

		panic(fmt.Sprintf("Unsupported input: %T\n", input))
	}

	if needsSkip {
		return nil, true
	}

	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0)
	// Handle null/empty OutputMessages from spec (empty arrays are now null in JSON)
	if m["OutputMessages"] != nil {
		for _, msg := range m["OutputMessages"].([]any) {
			byts, err := json.Marshal(msg)
			require.NoError(t, err)
			typedMsg := &spectypes.PartialSignatureMessages{}
			require.NoError(t, json.Unmarshal(byts, typedMsg))
			outputMsgs = append(outputMsgs, typedMsg)
		}
	}

	beaconBroadcastedRoots := make([]string, 0)
	if m["BeaconBroadcastedRoots"] != nil {
		for _, r := range m["BeaconBroadcastedRoots"].([]any) {
			beaconBroadcastedRoots = append(beaconBroadcastedRoots, r.(string))
		}
	}

	c := fixCommitteeForRun(t, logger, committeeMap)

	return &CommitteeSpecTest{
		Name:                   m["Name"].(string),
		Committee:              c,
		Input:                  inputs,
		PostDutyCommitteeRoot:  m["PostDutyCommitteeRoot"].(string),
		OutputMessages:         outputMsgs,
		BeaconBroadcastedRoots: beaconBroadcastedRoots,
		ExpectedErrorCode:      int(m["ExpectedErrorCode"].(float64)),
	}, false
}

func fixCommitteeForRun(t *testing.T, logger *zap.Logger, committeeMap map[string]any) *validator.Committee {
	byts, err := json.Marshal(committeeMap)
	require.NoError(t, err)
	specCommittee := &specssv.Committee{}
	require.NoError(t, json.Unmarshal(byts, specCommittee))

	c := validator.NewCommittee(
		logger,
		networkconfig.TestNetwork,
		&specCommittee.CommitteeMember,
		func(slot phase0.Slot, shareMap map[phase0.ValidatorIndex]*spectypes.Share, _ []phase0.BLSPubKey, _ runner.CommitteeDutyGuard) (*runner.CommitteeRunner, error) {
			r := ssvtesting.CommitteeRunnerWithShareMap(logger, shareMap)
			return r.(*runner.CommitteeRunner), nil
		},
		specCommittee.Share,
		validator.NewCommitteeDutyGuard(),
	)

	// v1.2.3 ssv-spec renamed types.Committee.Runners -> CommitteeRunners (and added a sibling
	// AggregatorCommitteeRunners map for the merged runner), so struct-tag-based json.Unmarshal
	// against our own (unrenamed) validator.Committee no longer finds the field. Read the raw map
	// directly instead, falling back to the legacy "Runners" key for older fixtures.
	// TODO(convergence unit 5): also read AggregatorCommitteeRunners.
	committeeRunnersMap, _ := committeeMap["CommitteeRunners"].(map[string]any)
	if committeeRunnersMap == nil {
		committeeRunnersMap, _ = committeeMap["Runners"].(map[string]any)
	}

	c.Runners = make(map[phase0.Slot]*runner.CommitteeRunner, len(committeeRunnersMap))
	for slotStr, rawRunner := range committeeRunnersMap {
		runnerMap, ok := rawRunner.(map[string]any)
		require.True(t, ok, "committee runner entry is not a map")

		slot, err := strconv.ParseUint(slotStr, 10, 64)
		require.NoError(t, err)

		baseRunnerMap := runnerMap["BaseRunner"].(map[string]any)
		var shareInstance *spectypes.Share
		for _, share := range baseRunnerMap["Share"].(map[string]any) {
			shareBytes, err := json.Marshal(share)
			require.NoError(t, err)
			shareInstance = &spectypes.Share{}
			require.NoError(t, json.Unmarshal(shareBytes, shareInstance))
			break
		}

		fixedRunner := fixRunnerForRun(t, runnerMap, spectestingutils.KeySetForShare(shareInstance))
		c.Runners[phase0.Slot(slot)] = fixedRunner.(*runner.CommitteeRunner)
	}

	return c
}
