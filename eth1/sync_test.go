package eth1

import (
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
	"time"
)

func TestSyncEth1(t *testing.T) {
	logger, eth1Client, storage := setupStorageWithEth1ClientMock()

	rawOffset := DefaultSyncOffset().Uint64()
	rawOffset += 10
	go func() {
		logs := []types.Log{{BlockNumber: rawOffset - 1}, {BlockNumber: rawOffset}}
		eth1Client.sub.Notify(Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.sub.Notify(Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.sub.Notify(Event{Data: SyncEndedEvent{Logs: logs, Success: true}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, "Eth1SyncTest", nil)
	require.NoError(t, err)
	syncOffset, err := storage.GetSyncOffset()
	require.NoError(t, err)
	require.NotNil(t, syncOffset)
	require.Equal(t, syncOffset.Uint64(), rawOffset)
}

func TestFailedSyncEth1(t *testing.T) {
	logger, eth1Client, storage := setupStorageWithEth1ClientMock()
	eth1Client.syncResponse = errors.New("eth1-sync-test")
	go func() {
		logs := []types.Log{{}, {BlockNumber: DefaultSyncOffset().Uint64()}}
		eth1Client.sub.Notify(Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.sub.Notify(Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.sub.Notify(Event{Data: SyncEndedEvent{Logs: logs, Success: false}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, "FailedEth1SyncTest", nil)
	require.EqualError(t, err, "failed to sync contract events: eth1-sync-test")

	_, err = storage.GetSyncOffset()
	require.NotNil(t, err)
}

func TestDetermineSyncOffset(t *testing.T) {
	logger := zap.L()

	t.Run("default sync offset", func(t *testing.T) {
		storage := syncStorageMock{[]byte{}}
		so := determineSyncOffset(logger, &storage, nil)
		require.NotNil(t, so)
		require.Equal(t, defaultSyncOffset, so.Text(16))
	})

	t.Run("persisted sync offset", func(t *testing.T) {
		storage := syncStorageMock{[]byte{}}
		so := new(SyncOffset)
		persistedSyncOffset := "60e08f"
		so.SetString(persistedSyncOffset, 16)
		storage.SaveSyncOffset(so)
		so = determineSyncOffset(logger, &storage, nil)
		require.NotNil(t, so)
		require.Equal(t, persistedSyncOffset, so.Text(16))
	})

	t.Run("sync offset from config", func(t *testing.T) {
		storage := syncStorageMock{[]byte{}}
		soConfig := new(SyncOffset)
		soConfig.SetString("61e08f", 16)
		so := determineSyncOffset(logger, &storage, soConfig)
		require.NotNil(t, so)
		require.Equal(t, "61e08f", so.Text(16))
	})
}

func setupStorageWithEth1ClientMock() (*zap.Logger, *eth1ClientMock, *syncStorageMock) {
	logger := zap.L()
	eth1Client := eth1ClientMock{pubsub.NewSubject(), 50 * time.Millisecond, nil}
	storage := syncStorageMock{[]byte{}}
	return logger, &eth1Client, &storage
}

type eth1ClientMock struct {
	sub pubsub.Subject

	syncTimeout  time.Duration
	syncResponse error
}

func (ec *eth1ClientMock) EventsSubject() pubsub.Subscriber {
	return ec.sub
}

func (ec *eth1ClientMock) Start() error {
	return nil
}

func (ec *eth1ClientMock) Sync(fromBlock *big.Int) error {
	<- time.After(ec.syncTimeout)
	return ec.syncResponse
}

type syncStorageMock struct {
	syncOffset []byte
}

// SaveSyncOffset saves the offset
func (ssm *syncStorageMock) SaveSyncOffset(offset *SyncOffset) error {
	ssm.syncOffset = offset.Bytes()
	return nil
}

// GetSyncOffset returns the offset
func (ssm *syncStorageMock) GetSyncOffset() (*SyncOffset, error) {
	if len(ssm.syncOffset) == 0 {
		return nil, errors.New(kv.EntryNotFoundError)
	}
	offset := new(SyncOffset)
	offset.SetBytes(ssm.syncOffset)
	return offset, nil
}
