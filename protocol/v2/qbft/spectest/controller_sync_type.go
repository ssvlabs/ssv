package qbft

import (
	"encoding/hex"
	"testing"

	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/controller/futuremsg"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

func RunControllerSync(t *testing.T, test *futuremsg.ControllerSyncSpecTest) {
	identifier := spectypes.NewMsgID(testingutils.TestingValidatorPubKey[:], spectypes.BNRoleAttester)
	config := testingutils.TestingConfig(testingutils.Testing4SharesSet())
	contr := NewTestingQBFTController(
		identifier[:],
		testingutils.TestingShare(testingutils.Testing4SharesSet()),
		config,
	)

	err := contr.StartNewInstance([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf(err.Error())
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		_, err := contr.ProcessMsg(msg)
		if err != nil {
			lastErr = err
		}
	}

	syncedDecidedCnt := config.GetNetwork().(*testingutils.TestingNetwork).SyncHighestDecidedCnt
	require.EqualValues(t, test.SyncDecidedCalledCnt, syncedDecidedCnt)

	r, err := contr.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.ControllerPostRoot, hex.EncodeToString(r))

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
