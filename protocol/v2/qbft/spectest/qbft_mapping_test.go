package qbft

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/ssvlabs/ssv-spec/qbft/spectest/tests/timeout"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	testing2 "github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

func TestQBFTMapping(t *testing.T) {
	path, _ := os.Getwd()
	jsonTests, err := protocoltesting.GenerateSpecTestJSON(path, "qbft")
	require.NoError(t, err)

	untypedTests := map[string]interface{}{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	for name, test := range untypedTests {
		name, test := name, test
		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]

		switch testType {
		case reflect.TypeOf(&spectests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunMsgProcessing(t, typedTest)
			})
		case reflect.TypeOf(&spectests.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunMsg(t, typedTest)
			})
		case reflect.TypeOf(&spectests.ControllerSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.ControllerSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				RunControllerSpecTest(t, typedTest)
			})
		case reflect.TypeOf(&spectests.CreateMsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &CreateMsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				t.Parallel()
				typedTest.RunCreateMsg(t)
			})
		case reflect.TypeOf(&spectests.RoundRobinSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.RoundRobinSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) { // using only spec struct so no need to run our version (TODO: check how we choose leader)
				t.Parallel()
				typedTest.Run(t)
			})
			/*t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsg(t, typedTest)
			})*/
		case reflect.TypeOf(&timeout.SpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &SpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			// a little trick we do to instantiate all the internal instance params

			preByts, _ := typedTest.Pre.Encode()
			logger := logging.TestLogger(t)
			ks := testingutils.Testing4SharesSet()
			signer := testingutils.NewOperatorSigner(ks, 1)
			pre := instance.NewInstance(
				testing2.TestingConfig(logger, testingutils.KeySetForCommitteeMember(typedTest.Pre.State.CommitteeMember)),
				typedTest.Pre.State.CommitteeMember,
				typedTest.Pre.State.ID,
				typedTest.Pre.State.Height,
				signer,
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
