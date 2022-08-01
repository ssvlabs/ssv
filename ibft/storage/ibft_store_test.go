package storage

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
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
	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	var identifierAtomic, height, round, preparedRound, preparedValue, iv atomic.Value
	height.Store(specqbft.Height(10))
	round.Store(specqbft.Round(2))
	identifierAtomic.Store(identifier[:])
	preparedRound.Store(specqbft.Round(8))
	preparedValue.Store([]byte("value"))
	iv.Store([]byte("input"))

	state := &qbft.State{
		Stage:         *atomic.NewInt32(int32(qbft.RoundStateDecided)),
		Identifier:    identifierAtomic,
		Height:        height,
		InputValue:    iv,
		Round:         round,
		PreparedRound: preparedRound,
		PreparedValue: preparedValue,
	}

	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	require.NoError(t, storage.SaveCurrentInstance(identifier[:], state))

	savedState, found, err := storage.GetCurrentInstance(identifier[:])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, specqbft.Height(10), savedState.GetHeight())
	require.Equal(t, specqbft.Round(2), savedState.GetRound())
	require.Equal(t, identifier.String(), message.ToMessageID(savedState.GetIdentifier()).String())
	require.Equal(t, specqbft.Round(8), savedState.GetPreparedRound())
	require.Equal(t, []byte("value"), savedState.GetPreparedValue())
	require.Equal(t, []byte("input"), savedState.GetInputValue())
}

func newTestIbftStorage(logger *zap.Logger, prefix string, forkVersion forksprotocol.ForkVersion) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger.With(zap.String("who", "badger")),
		Path:   "",
	})
	if err != nil {
		return nil, err
	}
	return New(db, logger.With(zap.String("who", "ibftStorage")), prefix, forkVersion), nil
}
