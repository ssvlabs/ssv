package storage

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestCleanDecided(t *testing.T) {
	msgID := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id spectypes.MessageID, h specqbft.Height) *specqbft.SignedMessage {
		return &specqbft.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []spectypes.OperatorID{1},
			Message: &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     h,
				Round:      1,
				Identifier: id[:],
				Data:       nil,
			},
		}
	}

	msgsCount := 10
	for i := 0; i < msgsCount; i++ {
		require.NoError(t, storage.SaveDecided(generateMsg(msgID, specqbft.Height(i))))
	}
	require.NoError(t, storage.SaveLastDecided(generateMsg(msgID, specqbft.Height(msgsCount))))

	// add different msgID
	differMsgID := spectypes.NewMsgID([]byte("differ_pk"), spectypes.BNRoleAttester)
	require.NoError(t, storage.SaveDecided(generateMsg(differMsgID, specqbft.Height(1))))
	require.NoError(t, storage.SaveLastDecided(generateMsg(differMsgID, specqbft.Height(msgsCount))))

	res, err := storage.GetDecided(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, msgsCount, len(res))

	last, err := storage.GetLastDecided(msgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, specqbft.Height(msgsCount), last.Message.Height)

	// remove all decided
	require.NoError(t, storage.CleanAllDecided(msgID[:]))
	res, err = storage.GetDecided(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	last, err = storage.GetLastDecided(msgID[:])
	require.NoError(t, err)
	require.Nil(t, last)

	// check other msgID
	res, err = storage.GetDecided(differMsgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

	last, err = storage.GetLastDecided(differMsgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
}

func TestIbftStorage_CleanLastChangeRound(t *testing.T) {
	msgID := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	differMsgID := spectypes.NewMsgID([]byte("pk_differ"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id spectypes.MessageID, h specqbft.Height, r specqbft.Round, s spectypes.OperatorID) *specqbft.SignedMessage {
		return &specqbft.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []spectypes.OperatorID{s},
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     h,
				Round:      r,
				Identifier: id[:],
				Data:       nil,
			},
		}
	}

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 1)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 2)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 3)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 4)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(differMsgID, 0, 1, 1))) // different identifier

	require.NoError(t, storage.CleanLastChangeRound(msgID[:]))
	res, err := storage.GetLastChangeRoundMsg(msgID[:])
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	res, err = storage.GetLastChangeRoundMsg(differMsgID[:])
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

}

func TestSaveAndFetchLastChangeRound(t *testing.T) {
	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id spectypes.MessageID, h specqbft.Height, r specqbft.Round, s spectypes.OperatorID) *specqbft.SignedMessage {
		return &specqbft.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []spectypes.OperatorID{s},
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     h,
				Round:      r,
				Identifier: id[:],
				Data:       nil,
			},
		}
	}

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 1, 1)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 2)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 3, 3)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 4, 4)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester), 0, 4, 4))) // different identifier

	res, err := storage.GetLastChangeRoundMsg(identifier[:])
	require.NoError(t, err)
	require.Equal(t, 4, len(res))

	// check signer 1
	require.Equal(t, spectypes.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, specqbft.Round(1), res[0].Message.Round)
	require.Equal(t, specqbft.Height(0), res[0].Message.Height)
	// check signer 3
	msg3 := res[2]
	require.Equal(t, spectypes.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, specqbft.Round(3), msg3.Message.Round)
	require.Equal(t, specqbft.Height(0), msg3.Message.Height)

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 1))) // update round for signer 1
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 1, 2, 3))) // update round for signer 3

	res, err = storage.GetLastChangeRoundMsg(identifier[:])
	require.NoError(t, err)

	require.Equal(t, 4, len(res))
	require.Equal(t, spectypes.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, specqbft.Round(2), res[0].Message.Round)

	// check signer 3
	msg3 = res[2]
	require.Equal(t, spectypes.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, specqbft.Round(2), msg3.Message.Round)
	require.Equal(t, specqbft.Height(1), msg3.Message.Height)

}

func TestSaveAndFetchLastState(t *testing.T) {
	id := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)

	state := &specqbft.State{
		ID:                id[:],
		Height:            10,
		Round:             2,
		LastPreparedRound: 8,
		LastPreparedValue: []byte("value"),
	}

	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	require.NoError(t, storage.SaveCurrentInstance(id[:], state))

	savedState, found, err := storage.GetCurrentInstance(id[:])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, specqbft.Height(10), savedState.Height)
	require.Equal(t, specqbft.Round(2), savedState.Round)
	require.Equal(t, id.String(), message.ToMessageID(savedState.ID).String())
	require.Equal(t, specqbft.Round(8), savedState.LastPreparedRound)
	require.Equal(t, []byte("value"), savedState.LastPreparedValue)
}

func newTestIbftStorage(logger *zap.Logger, prefix string, forkVersion forksprotocol.ForkVersion) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:      "badger-memory",
		Logger:    logger.With(zap.String("who", "badger")),
		Path:      "",
		Reporting: true,
	})
	if err != nil {
		return nil, err
	}
	return New(db, logger.With(zap.String("who", "ibftStorage")), prefix, forkVersion), nil
}
