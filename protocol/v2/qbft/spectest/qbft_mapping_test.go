package qbft

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	specqbfttests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/controller/futuremsg"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("qbft-mapping-test", zapcore.DebugLevel, nil)
}

func TestQBFTMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	filePath := path + "/" + fileName
	jsonTests, err := os.ReadFile(filePath)
	if err != nil {
		resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/v0.2.7/qbft/spectest/generate/tests.json")
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

		switch testType {
		case reflect.TypeOf(&specqbfttests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &specqbfttests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsgProcessing(t, typedTest)
			})
		case reflect.TypeOf(&specqbfttests.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &specqbfttests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&specqbfttests.ControllerSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &specqbfttests.ControllerSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&specqbfttests.CreateMsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &specqbfttests.CreateMsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&specqbfttests.RoundRobinSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &specqbfttests.RoundRobinSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) { // using only spec struct so no need to run our version (TODO: check how we choose leader)
				typedTest.Run(t)
			})
			/*t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsg(t, typedTest)
			})*/

		case reflect.TypeOf(&futuremsg.ControllerSyncSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &futuremsg.ControllerSyncSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		default:
			t.Fatalf("unsupported test type %s [%s]", testType, testName)
		}
	}
}
