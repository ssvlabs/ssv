package storage

import (
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestSaveAndFetchLastChangeRound(t *testing.T) {
	identifier := message.NewIdentifier([]byte("pk"), message.RoleTypeAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id message.Identifier, h message.Height, r message.Round, s message.OperatorID) *message.SignedMessage {
		return &message.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []message.OperatorID{s},
			Message: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     h,
				Round:      r,
				Identifier: id,
				Data:       nil,
			},
		}
	}

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 1, 1)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 2)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 3, 3)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 4, 4)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(message.NewIdentifier([]byte("pk"), message.RoleTypeAttester), 0, 4, 4))) // different identifier

	res, err := storage.GetLastChangeRoundMsg(identifier)
	require.NoError(t, err)
	require.Equal(t, 4, len(res))

	// check signer 1
	require.Equal(t, message.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, message.Round(1), res[0].Message.Round)
	require.Equal(t, message.Height(0), res[0].Message.Height)
	// check signer 3
	msg3 := res[2]
	require.Equal(t, message.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, message.Round(3), msg3.Message.Round)
	require.Equal(t, message.Height(0), msg3.Message.Height)

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 1))) // update round for signer 1
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 1, 2, 3))) // update round for signer 3

	res, err = storage.GetLastChangeRoundMsg(identifier)
	require.NoError(t, err)

	require.Equal(t, 4, len(res))
	require.Equal(t, message.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, message.Round(2), res[0].Message.Round)

	// check signer 3
	msg3 = res[2]
	require.Equal(t, message.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, message.Round(2), msg3.Message.Round)
	require.Equal(t, message.Height(1), msg3.Message.Height)

}

func TestSaveAndFetchLastState(t *testing.T) {
	identifier := message.NewIdentifier([]byte("pk"), message.RoleTypeAttester)
	var identifierAtomic, height, round, preparedRound, preparedValue, iv atomic.Value
	height.Store(message.Height(10))
	round.Store(message.Round(2))
	identifierAtomic.Store(identifier)
	preparedRound.Store(message.Round(8))
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

	require.NoError(t, storage.SaveCurrentInstance(identifier, state))

	savedState, found, err := storage.GetCurrentInstance(identifier)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, message.Height(10), savedState.GetHeight())
	require.Equal(t, message.Round(2), savedState.GetRound())
	require.Equal(t, identifier.String(), savedState.GetIdentifier().String())
	require.Equal(t, message.Round(8), savedState.GetPreparedRound())
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
