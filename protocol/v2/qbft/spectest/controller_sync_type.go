package qbft

import (
	"encoding/hex"
	"testing"

	qbfttesting "github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	"github.com/bloxapp/ssv/protocol/v2/types"

	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/controller/futuremsg"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/stretchr/testify/require"
)

func RunControllerSync(t *testing.T, test *futuremsg.ControllerSyncSpecTest) {
	logger := logging.TestLogger(t)
	identifier := spectypes.NewMsgID(types.GetDefaultDomain(), spectestingutils.TestingValidatorPubKey[:], spectypes.BNRoleAttester)
	config := qbfttesting.TestingConfig(logger, spectestingutils.Testing4SharesSet(), identifier.GetRoleType())
	contr := qbfttesting.NewTestingQBFTController(
		identifier[:],
		spectestingutils.TestingShare(spectestingutils.Testing4SharesSet()),
		config,
		false,
	)

	err := contr.StartNewInstance(logger, 0, []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf(err.Error())
	}

	var lastErr error
	for _, msg := range test.InputMessages {
		logger = logger.With(fields.Height(msg.Message.Height))
		_, err := contr.ProcessMsg(logger, msg)
		if err != nil {
			lastErr = err
		}
	}

	syncedDecidedCnt := config.GetNetwork().(*spectestingutils.TestingNetwork).SyncHighestDecidedCnt
	require.EqualValues(t, test.SyncDecidedCalledCnt, syncedDecidedCnt)

	r, err := contr.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.ControllerPostRoot, hex.EncodeToString(r[:]))

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
