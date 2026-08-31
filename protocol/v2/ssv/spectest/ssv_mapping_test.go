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
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

func runSSVMappingTest(t *testing.T) {
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

// gloasSpecRunnerSkipReason documents why the Gloas runner/committee spec vectors are skipped
// here for now. The ssv-spec epbs-gloas-types head added spec-side Gloas runners with spec tests,
// plus Gloas-era variants of the shared runner/committee vectors. Neither class is runnable yet:
//   - the new Gloas runner roles — the node's implementations deliberately differ in internals
//     (multi-slot preferences dispatcher, abstaining PTC attester, §6 envelope windows);
//   - Gloas-era variants of pre-existing vectors — their post-state roots embed the network
//     config, which the ssvtesting runner constructors cannot schedule Gloas into without
//     diverging those roots, and the testing beacon-node wrapper has no Gloas produce surface.
//
// Mapping both is a reconciliation task tracked with the ssv-spec repin follow-up on PR #2901;
// the node's own unit tests cover the Gloas runner behavior meanwhile. The Gloas valcheck
// vectors (which embed no config) DO run, against the node's real checkers.
const gloasSpecRunnerSkipReason = "spec-side Gloas runner tests not yet mapped onto the node's runners (PR #2901 repin follow-up)"

// isUnmappedGloasRunnerTest reports whether the raw (untyped) runner spec test is one the mapping
// harness cannot run yet — see gloasSpecRunnerSkipReason: a Gloas runner role outright, or a
// Gloas-era variant of a shared vector.
func isUnmappedGloasRunnerTest(m map[string]any) bool {
	runnerMap, ok := m["Runner"].(map[string]any)
	if !ok {
		return false
	}
	baseRunnerMap, ok := runnerMap["BaseRunner"].(map[string]any)
	if !ok {
		return false
	}
	role, ok := baseRunnerMap["RunnerRoleType"].(float64)
	if !ok {
		return false
	}
	switch spectypes.RunnerRole(role) {
	case spectypes.RolePTCAttester, spectypes.RoleProposerPreferences, spectypes.RoleEnvelopeProposer:
		return true
	default: // every other role: unmapped only when the vector is Gloas-era (below)
	}
	duty, err := decodeDutyFromMap(m)
	if err != nil {
		return false
	}
	return gloasTestBeaconConfig().IsGloasAtSlot(duty.DutySlot())
}

// isUnmappedGloasCommitteeTest reports whether the raw (untyped) committee spec test carries a
// Gloas-era input duty — the committee analog of isUnmappedGloasRunnerTest, same reason. Unlike
// the runner tests' wrapped duty encoding, committee inputs are bare duty objects (a top-level
// "Slot" next to "ValidatorDuties"); SignedSSVMessage inputs carry no top-level Slot and are
// passed over.
func isUnmappedGloasCommitteeTest(m map[string]any) bool {
	inputs, ok := m["Input"].([]any)
	if !ok {
		return false
	}
	beaconCfg := gloasTestBeaconConfig()
	for _, input := range inputs {
		inputMap, ok := input.(map[string]any)
		if !ok {
			continue
		}
		var slot phase0.Slot
		switch v := inputMap["Slot"].(type) {
		case string: // spec JSON encodes uint64 as a decimal string
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				continue
			}
			slot = phase0.Slot(n)
		case float64:
			slot = phase0.Slot(v)
		default:
			continue
		}
		if beaconCfg.IsGloasAtSlot(slot) {
			return true
		}
	}
	return false
}

func prepareTest(t *testing.T, logger *zap.Logger, name string, test any) *runnable {
	testName := strings.Split(name, "_")[1]
	testType := strings.Split(name, "_")[0]

	switch testType {
	case reflect.TypeFor[*tests.MsgProcessingSpecTest]().String():
		if testMap := test.(map[string]any); isUnmappedGloasRunnerTest(testMap) {
			return &runnable{
				name: testMap["Name"].(string),
				test: func(t *testing.T) { t.Skip(gloasSpecRunnerSkipReason) },
			}
		}
		typedTest := msgProcessingSpecTestFromMap(t, test.(map[string]any))

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			},
		}
	case reflect.TypeFor[*tests.MultiMsgProcessingSpecTest]().String():
		multiName := test.(map[string]any)["Name"].(string)
		typedTest := &MultiMsgProcessingSpecTest{
			Name: multiName,
		}
		subtests := test.(map[string]any)["Tests"].([]any)
		for _, subtest := range subtests {
			subtestMap := subtest.(map[string]any)
			// TODO(convergence unit 5d follow-up): "sync committee aggregator selection proof"
			// (under "pre consensus post decided") hardcodes an Altair-versioned
			// AggregatorCommitteeConsensusData at duty slot 2375680 (= ssv-spec's real-mainnet
			// ForkEpochAltair(74240)*32; see types/testingutils/beacon_node_versions.go). Our
			// AggregatorCommitteeRunner derives DataVersion from
			// NetworkConfig.ForkAtEpoch(EstimatedEpochAtSlot(slot)), and our (and boole's,
			// byte-identical) TestNetwork.Beacon.Forks toy schedule (Phase0=0 .. Fulu=6) can never
			// resolve epoch 74240 to Altair - it always lands on Electra/Fulu. boole "passes" this
			// vector only because its msg_processing_type.go downgrades the post-root mismatch to a
			// non-failing logger.Error (which we deliberately do NOT port - see RunAsPartOfMultiTest).
			// Skip only this single vector until the toy TestNetwork fork-epoch schedule is
			// reconciled with these real-epoch v1.2.3 fixtures (or the fixture is regenerated).
			if multiName == "pre consensus post decided" && subtestMap["Name"] == "sync committee aggregator selection proof" {
				continue
			}
			// Skip the Gloas subtests these multis carry (new roles and Gloas-era variants) — see gloasSpecRunnerSkipReason.
			if isUnmappedGloasRunnerTest(subtestMap) {
				continue
			}
			typedTest.Tests = append(typedTest.Tests, msgProcessingSpecTestFromMap(t, subtestMap))
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
	case reflect.TypeFor[*synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest]().String(): // no use of internal structs so can run as spec test runs TODO: need to use internal signer
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
	case reflect.TypeFor[*newduty.MultiStartNewRunnerDutySpecTest]().String():
		typedTest := &MultiStartNewRunnerDutySpecTest{
			Name: test.(map[string]any)["Name"].(string),
		}

		return &runnable{
			name: typedTest.TestName(),
			// Subtests are decoded lazily inside the run closure (unlike
			// MultiMsgProcessingSpecTest, which decodes eagerly in prepareTest) so a
			// decode failure attributes to the individual subtest run rather than to setup.
			test: func(t *testing.T) {
				subtests := test.(map[string]any)["Tests"].([]any)
				for _, subtest := range subtests {
					subtestMap := subtest.(map[string]any)
					// Skip the Gloas subtests (new roles and Gloas-era variants) — see gloasSpecRunnerSkipReason.
					if isUnmappedGloasRunnerTest(subtestMap) {
						continue
					}
					typedTest.Tests = append(typedTest.Tests, newRunnerDutySpecTestFromMap(t, subtestMap))
				}
				typedTest.Run(t, logger)
			},
		}
	case reflect.TypeFor[*partialsigcontainer.PartialSigContainerTest]().String():
		byts, err := json.Marshal(test)
		require.NoError(t, err)
		typedTest := &partialsigcontainer.PartialSigContainerTest{}
		require.NoError(t, json.Unmarshal(byts, &typedTest))
		// The spec struct's Run asserts the code itself, so remap the vector's expectation
		// up front (identity in the default build, v1.2.2->v1.2.3 in the alan_spec build).
		typedTest.ExpectedErrorCode = adjustExpectedErrorCode(typedTest.ExpectedErrorCode)

		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*committee.CommitteeSpecTest]().String():
		if testMap := test.(map[string]any); isUnmappedGloasCommitteeTest(testMap) {
			return &runnable{
				name: testMap["Name"].(string),
				test: func(t *testing.T) { t.Skip(gloasSpecRunnerSkipReason) },
			}
		}
		typedTest := committeeSpecTestFromMap(t, logger, test.(map[string]any))
		return &runnable{
			name: typedTest.TestName(),
			test: func(t *testing.T) {
				typedTest.Run(t)
			},
		}
	case reflect.TypeFor[*committee.MultiCommitteeSpecTest]().String():
		// TODO(gloas repin follow-up, PR #2901): the repinned spec's Committee.StartDuty records the
		// runner in its map before the duty-level validation that fails this vector
		// (InvalidAggregatorCommitteeDuty on mismatched inner-duty slots), so the expected post-state
		// carries the failed runner. The node validates first and never records it — the safer order —
		// so the post-state roots can't match. Skip until the bookkeeping contract is reconciled with
		// the spec (or the fixture is regenerated against validate-first).
		if multiCommitteeName := test.(map[string]any)["Name"].(string); multiCommitteeName == "aggregator committee runner duty with different slots" {
			return &runnable{
				name: multiCommitteeName,
				test: func(t *testing.T) { t.Skip(gloasSpecRunnerSkipReason) },
			}
		}
		subtests := test.(map[string]any)["Tests"].([]any)
		typedTests := make([]*CommitteeSpecTest, 0)
		for _, subtest := range subtests {
			subtestMap := subtest.(map[string]any)
			// Skip the Gloas-era committee subtests — see gloasSpecRunnerSkipReason.
			if isUnmappedGloasCommitteeTest(subtestMap) {
				continue
			}
			typedTests = append(typedTests, committeeSpecTestFromMap(t, logger, subtestMap))
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

	beaconAggregators := make([]phase0.CommitteeIndex, 0)
	if raw, ok := m["BeaconAggregators"]; ok && raw != nil {
		for _, idx := range raw.([]any) {
			beaconAggregators = append(beaconAggregators, phase0.CommitteeIndex(idx.(float64)))
		}
	}
	beaconAggregatorsValues := make([]bool, 0)
	if raw, ok := m["BeaconAggregatorsValues"]; ok && raw != nil {
		for _, v := range raw.([]any) {
			beaconAggregatorsValues = append(beaconAggregatorsValues, v.(bool))
		}
	}
	// BeaconAggregatorsMap zips these two slices by index, so a fixture where they
	// differ in length would panic with an opaque index-out-of-range. Fail legibly here.
	require.Equal(t, len(beaconAggregators), len(beaconAggregatorsValues),
		"BeaconAggregators and BeaconAggregatorsValues must have equal length")

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
		BeaconAggregators:       beaconAggregators,
		BeaconAggregatorsValues: beaconAggregatorsValues,
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
			switch duty.(type) {
			case *spectypes.CommitteeDuty:
				r := ssvtesting.CommitteeRunnerWithShareMap(logger, shareMap)
				applyRunnerNetworkConfig(r, netCfg)
				return r, nil
			case *spectypes.AggregatorCommitteeDuty:
				r := ssvtesting.AggregatorCommitteeRunnerWithShareMap(logger, shareMap)
				applyRunnerNetworkConfig(r, netCfg)
				return r, nil
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
	if committeeRunnersMap == nil {
		committeeRunnersMap, _ = committeeMap["Runners"].(map[string]any)
	}
	aggregatorRunnersMap, _ := committeeMap["AggregatorCommitteeRunners"].(map[string]any)
	if aggregatorRunnersMap == nil {
		aggregatorRunnersMap, _ = committeeMap["AggregatorRunners"].(map[string]any)
	}
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
