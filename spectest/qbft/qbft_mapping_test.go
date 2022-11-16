package qbft

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bloxapp/ssv-spec/qbft/spectest"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestQBFTMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	filePath := path + "/" + fileName
	jsonTests, err := os.ReadFile(filePath)
	if err != nil {
		//resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/V0.2/qbft/spectest/generate/tests.json")
		resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/qbft_sync_v0.2.1/qbft/spectest/generate/tests.json")
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

	tests := make(map[string]spectest.SpecTest)
	for name, test := range untypedTests {
		logex.Reset()
		name, test := name, test

		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]

		if _, ok := excludeTest()[testName]; ok { // test that not passing
			continue
		}

		switch testType {
		case reflect.TypeOf(&spectests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsgProcessingSpecTest(t, typedTest)
			})
		case reflect.TypeOf(&spectests.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				RunMsgSpecTest(t, typedTest)
			})
			//default:
			//	t.Fatalf("unsupported test type %s [%s]", testType, testName)
		}
	}
}

func excludeTest() map[string]bool {
	return map[string]bool{
		//consensus.FutureDecided().TestName():             true, // multi instance required
		//consensus.MultiSignerNotDecidedMsg().TestName():  true,
		//consensus.InvalidDecidedSig().TestName():         true,
		//consensus.InvalidIdentifier().TestName():         true, // missing error
		//consensus.ProcessMsgError().TestName():           true, // missing error
		//consensus.StartInstanceInvalidValue().TestName(): true, // missing error
		//consensus.InvalidSig().TestName():                true, // missing error
		//consensus.QueueCleanup().TestName():              true, // sync count
		//consensus.F1HighestDecidedSync().TestName():      true, // sync count
	}
}
