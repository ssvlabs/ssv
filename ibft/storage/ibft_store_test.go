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
		Stage:         *atomic.NewInt32(int32(qbft.RoundState_Decided)),
		Identifier:    identifierAtomic,
		Height:        height,
		InputValue:    iv,
		Round:         round,
		PreparedRound: preparedRound,
		PreparedValue: preparedValue,
	}

	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.V1ForkVersion)
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
