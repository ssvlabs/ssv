package qbft

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/ssvlabs/ssv-spec/qbft/spectest/tests/timeout"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

// runQBFTMappingTest is the shared suite body; the TestQBFTMapping / TestQBFTMappingAlan
// entry points (build-tag split) select whether it runs against the Boole or the Alan
// (pre-fork) spec vectors.
func runQBFTMappingTest(t *testing.T) {
	path, _ := os.Getwd()
	jsonTests, err := storage.GenerateSpecTestJSON(path, "qbft")
	require.NoError(t, err)

	untypedTests := map[string]any{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	for name, test := range untypedTests {
		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]

		switch testType {
		case reflect.TypeFor[*spectests.MsgProcessingSpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunMsgProcessing(t, typedTest)
			})
		case reflect.TypeFor[*spectests.MsgSpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunMsg(t, typedTest)
			})
		case reflect.TypeFor[*spectests.ControllerSpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.ControllerSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunControllerSpecTest(t, typedTest)
			})
		case reflect.TypeFor[*spectests.CreateMsgSpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &CreateMsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				typedTest.RunCreateMsg(t)
			})
		case reflect.TypeFor[*spectests.RoundRobinSpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.RoundRobinSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				// Build-tag split: the default build runs the spec's own struct, the alan_spec
				// build drives our pre-Boole proposer selection against the Alan vectors.
				runRoundRobinSpecTest(t, typedTest)
			})
		case reflect.TypeFor[*timeout.SpecTest]().String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &SpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			// a little trick we do to instantiate all the internal instance params

			preByts, _ := typedTest.Pre.Encode()
			logger := protocoltesting.SpectestLogger(t)
			ks := testingutils.Testing4SharesSet()
			signer := testingutils.NewOperatorSigner(ks, 1)
			pre := instance.NewInstance(
				t.Context(),
				logger,
				protocoltesting.TestingConfig(logger, testingutils.KeySetForCommitteeMember(typedTest.Pre.State.CommitteeMember)),
				typedTest.Pre.State.CommitteeMember,
				typedTest.Pre.State.ID,
				typedTest.Pre.State.Height,
				signer,
				func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
					return roundtimer.NewTestingTimer()
				},
			)
			err = pre.Decode(preByts)
			require.NoError(t, err)
			typedTest.Pre = pre
			t.Run(typedTest.Name, func(t *testing.T) { // using only spec struct so no need to run our version (TODO: check how we choose leader)
				t.Parallel()
				RunTimeout(t, typedTest)
			})

		default:
			t.Fatalf("unsupported test type %s [%s]", testType, testName)
		}
	}
}
