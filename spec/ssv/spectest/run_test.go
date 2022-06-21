package spectest

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/spec/qbft"
	tests2 "github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
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
	require.NoError(t, err)

	if err := json.Unmarshal(byteValue, &tests); err != nil {
		require.NoError(t, err)
	}

	for _, test := range tests {

		// a little trick we do to instantiate all the internal controller params
		byts, err := test.Runner.QBFTController.Encode()
		require.NoError(t, err)

		ks := keySetForShare(test.Runner.QBFTController.Share)

		newContr := qbft.NewController(
			[]byte{1, 2, 3, 4},
			test.Runner.QBFTController.Share,
			testingutils.TestingConfig(ks).Domain,
			testingutils.TestingConfig(ks).Signer,
			testingutils.TestingConfig(ks).ValueCheck,
			testingutils.TestingConfig(ks).Storage,
			testingutils.TestingConfig(ks).Network,
		)
		require.NoError(t, newContr.Decode(byts))
		test.Runner.QBFTController = newContr

		for idx, i := range test.Runner.QBFTController.StoredInstances {
			if i == nil {
				continue
			}
			fixedInst := fixQBFTInstanceForRun(t, i, ks)
			test.Runner.QBFTController.StoredInstances[idx] = fixedInst

			if test.Runner.State != nil &&
				test.Runner.State.RunningInstance != nil &&
				test.Runner.State.RunningInstance.GetHeight() == fixedInst.GetHeight() {
				test.Runner.State.RunningInstance = fixedInst
			}
		}
		t.Run(test.Name, func(t *testing.T) {
			runTest(t, test)
		})
	}
}

func runTest(t *testing.T, test *tests2.SpecTest) {
	v := testingutils.BaseValidator(keySetForShare(test.Runner.Share))
	v.DutyRunners[test.Runner.BeaconRoleType] = test.Runner

	lastErr := v.StartDuty(test.Duty)
	for _, msg := range test.Messages {
		err := v.ProcessMessage(msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := test.Runner.State.GetRoot()
	require.NoError(t, err)

	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot))
}

func fixQBFTInstanceForRun(t *testing.T, i *qbft.Instance, ks *testingutils.TestKeySet) *qbft.Instance {
	// a little trick we do to instantiate all the internal instance params
	if i == nil {
		return nil
	}
	byts, _ := i.Encode()
	newInst := qbft.NewInstance(testingutils.TestingConfig(ks), i.State.Share, i.State.ID, qbft.FirstHeight)
	require.NoError(t, newInst.Decode(byts))
	return newInst
}

func keySetForShare(share *types.Share) *testingutils.TestKeySet {
	if share.Quorum == 5 {
		return testingutils.Testing7SharesSet()
	}
	if share.Quorum == 7 {
		return testingutils.Testing10SharesSet()
	}
	if share.Quorum == 9 {
		return testingutils.Testing13SharesSet()
	}
	return testingutils.Testing4SharesSet()
}
