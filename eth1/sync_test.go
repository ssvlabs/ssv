package eth1

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestSyncEth1(t *testing.T) {
	logger, eth1Client, storage := setupStorageWithEth1ClientMock()

	rawOffset := DefaultSyncOffset().Uint64()
	rawOffset += 10
	go func() {
		// wait 5 ms and start to push events
		time.Sleep(5 * time.Millisecond)
		logs := []types.Log{{BlockNumber: rawOffset - 1}, {BlockNumber: rawOffset}}
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.Feed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: true}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, nil, nil)
	require.NoError(t, err)
	syncOffset, _, err := storage.GetSyncOffset()
	require.NoError(t, err)
	require.NotNil(t, syncOffset)
	require.Equal(t, syncOffset.Uint64(), rawOffset)
}

func TestSyncEth1Error(t *testing.T) {
	logger, eth1Client, storage := setupStorageWithEth1ClientMock()
	eth1Client.SyncResponse = errors.New("eth1-sync-test")
	go func() {
		logs := []types.Log{{}, {BlockNumber: DefaultSyncOffset().Uint64()}}
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.Feed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: false}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, nil, nil)
	require.EqualError(t, err, "failed to sync contract events: eth1-sync-test")

	_, found, err := storage.GetSyncOffset()
	require.NoError(t, err)
	require.False(t, found)
}

func TestSyncEth1HandlerError(t *testing.T) {
	logger, eth1Client, storage := setupStorageWithEth1ClientMock()
	go func() {
		<-time.After(time.Millisecond * 25)
		logs := []types.Log{{BlockNumber: DefaultSyncOffset().Uint64() - 1}, {BlockNumber: DefaultSyncOffset().Uint64()}}
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eth1Client.Feed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eth1Client.Feed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: false}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, nil, func(event Event) error {
		return errors.New("test")
	})
	require.EqualError(t, err, "failed to handle all events from sync")
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

func setupStorageWithEth1ClientMock() (*zap.Logger, *ClientMock, *syncStorageMock) {
	logger := zap.L()
	eth1Client := ClientMock{Feed: new(event.Feed), SyncTimeout: 50 * time.Millisecond}
	storage := syncStorageMock{[]byte{}}
	return logger, &eth1Client, &storage
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
func (ssm *syncStorageMock) GetSyncOffset() (*SyncOffset, bool, error) {
	if len(ssm.syncOffset) == 0 {
		return nil, false, nil
	}
	offset := new(SyncOffset)
	offset.SetBytes(ssm.syncOffset)
	return offset, true, nil
}
