package spectest

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/spec/qbft"
	tests2 "github.com/bloxapp/ssv/spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv/spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestAll(t *testing.T) {
	for _, test := range AllTests {
		t.Run(test.Name, func(t *testing.T) {
			runTest(t, test)
		})
	}
}

func TestJson(t *testing.T) {
	basedir, _ := os.Getwd()
	path := filepath.Join(basedir, "generate")
	fileName := "tests.json"
	tests := map[string]*tests2.SpecTest{}
	byteValue, err := ioutil.ReadFile(path + "/" + fileName)
	if err != nil {
		panic(err.Error())
	}

	if err := json.Unmarshal(byteValue, &tests); err != nil {
		panic(err.Error())
	}

	for _, test := range tests {
		byts, _ := test.Pre.Encode()

		// a little trick we do to instantiate all the internal instance params
		pre := qbft.NewInstance(testingutils.TestingConfig(testingutils.Testing4SharesSet()), test.Pre.State.Share, test.Pre.State.ID, qbft.FirstHeight)
		pre.Decode(byts)
		test.Pre = pre
		t.Run(test.Name, func(t *testing.T) {
			runTest(t, test)
		})
	}
}

func runTest(t *testing.T, test *tests2.SpecTest) {
	var lastErr error
	for _, msg := range test.Messages {
		_, _, _, err := test.Pre.ProcessMsg(msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := test.Pre.State.GetRoot()
	require.NoError(t, err)

	require.EqualValues(t, test.PostRoot, hex.EncodeToString(postRoot), "post root not valid")
}
