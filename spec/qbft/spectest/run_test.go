package spectest

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/spec/qbft"
	tests2 "github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

func TestAll(t *testing.T) {
	for _, test := range AllTests {
		t.Run(test.TestName(), func(t *testing.T) {
			test.Run(t)
		})
	}
}

func TestJson(t *testing.T) {
	basedir, _ := os.Getwd()
	path := filepath.Join(basedir, "generate")
	fileName := "tests.json"
	untypedTests := map[string]interface{}{}
	byteValue, err := ioutil.ReadFile(path + "/" + fileName)
	if err != nil {
		panic(err.Error())
	}

	if err := json.Unmarshal(byteValue, &untypedTests); err != nil {
		panic(err.Error())
	}

	tests := make(map[string]SpecTest)
	for name, test := range untypedTests {
		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]
		switch testType {
		case reflect.TypeOf(&tests2.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &tests2.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			// a little trick we do to instantiate all the internal instance params
			preByts, _ := typedTest.Pre.Encode()
			pre := qbft.NewInstance(testingutils.TestingConfig(testingutils.Testing4SharesSet()), typedTest.Pre.State.Share, typedTest.Pre.State.ID, qbft.FirstHeight)
			pre.Decode(preByts)
			typedTest.Pre = pre

			tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		case reflect.TypeOf(&tests2.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &tests2.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				typedTest.Run(t)
			})
		}
	}
}
