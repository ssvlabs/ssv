package eth1

import (
	"errors"
	"math/big"
	"testing"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/golang/mock/gomock"
	"github.com/prysmaticlabs/prysm/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
)

func TestSyncEth1(t *testing.T) {
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eth1Client, eventsFeed := eth1ClientMock(logger, ctrl, nil)
	storage := syncStorageMock(ctrl)

	rawOffset := spectypes.GetBeaconTestNetwork().DefaultSyncOffset.Uint64()
	rawOffset += 10
	go func() {
		// wait 5 ms and start to push events
		time.Sleep(5 * time.Millisecond)
		logs := []types.Log{{BlockNumber: rawOffset - 1}, {BlockNumber: rawOffset}}
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eventsFeed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: true}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, spectypes.GetBeaconTestNetwork(), nil, nil)
	require.NoError(t, err)
	syncOffset, _, err := storage.GetSyncOffset()
	require.NoError(t, err)
	require.NotNil(t, syncOffset)
	require.Equal(t, syncOffset.Uint64(), rawOffset)
}

func TestSyncEth1Error(t *testing.T) {
	logger := logging.TestLogger(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eth1Client, eventsFeed := eth1ClientMock(logger, ctrl, errors.New("eth1-sync-test"))
	storage := syncStorageMock(ctrl)

	go func() {
		logs := []types.Log{{}, {BlockNumber: spectypes.GetBeaconTestNetwork().DefaultSyncOffset.Uint64()}}
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eventsFeed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: false}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, spectypes.GetBeaconTestNetwork(), nil, nil)
	require.EqualError(t, err, "failed to sync contract events: eth1-sync-test")

	_, found, err := storage.GetSyncOffset()
	require.NoError(t, err)
	require.False(t, found)
}

func TestSyncEth1HandlerError(t *testing.T) {
	logger := logging.TestLogger(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eth1Client, eventsFeed := eth1ClientMock(logger, ctrl, nil)
	storage := syncStorageMock(ctrl)

	go func() {
		<-time.After(time.Millisecond * 25)
		blockNumber := spectypes.GetBeaconTestNetwork().DefaultSyncOffset.Uint64()
		logs := []types.Log{{BlockNumber: blockNumber - 1}, {BlockNumber: blockNumber}}
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[0]})
		eventsFeed.Send(&Event{Data: struct{}{}, Log: logs[1]})
		eventsFeed.Send(&Event{Data: SyncEndedEvent{Logs: logs, Success: false}})
	}()
	err := SyncEth1Events(logger, eth1Client, storage, spectypes.GetBeaconTestNetwork(), nil, func(event Event) ([]zap.Field, error) {
		return nil, errors.New("test")
	})
	require.EqualError(t, err, "could not handle some of the events during history sync")
}

func TestDetermineSyncOffset(t *testing.T) {
	logger := logging.TestLogger(t)
	beaconNetwork := spectypes.GetBeaconTestNetwork()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("default sync offset", func(t *testing.T) {
		storage := syncStorageMock(ctrl)

		so := determineSyncOffset(logger, storage, beaconNetwork, nil)
		require.NotNil(t, so)
		require.Equal(t, beaconNetwork.DefaultSyncOffset, so.Text(10))
	})

	t.Run("persisted sync offset", func(t *testing.T) {
		storage := syncStorageMock(ctrl)
		so := new(SyncOffset)
		persistedSyncOffset := "60e08f"
		so.SetString(persistedSyncOffset, 16)
		require.NoError(t, storage.SaveSyncOffset(so))
		so = determineSyncOffset(logger, storage, beaconNetwork, nil)
		require.NotNil(t, so)
		require.Equal(t, persistedSyncOffset, so.Text(16))
	})

	t.Run("sync offset from config", func(t *testing.T) {
		storage := syncStorageMock(ctrl)
		soConfig := new(SyncOffset)
		soConfig.SetString("61e08f", 16)
		so := determineSyncOffset(logger, storage, beaconNetwork, soConfig)
		require.NotNil(t, so)
		require.Equal(t, "61e08f", so.Text(16))
	})
}

func eth1ClientMock(logger *zap.Logger, ctrl *gomock.Controller, err error) (*MockClient, *event.Feed) {
	eventsFeed := new(event.Feed)

	eth1Client := NewMockClient(ctrl)
	eth1Client.EXPECT().EventsFeed().Return(eventsFeed)
	eth1Client.EXPECT().Sync(logger, gomock.Any()).DoAndReturn(func(*zap.Logger, *big.Int) error {
		<-time.After(50 * time.Millisecond)
		return err
	})
	return eth1Client, eventsFeed
}

func syncStorageMock(ctrl *gomock.Controller) *MockSyncOffsetStorage {
	syncOffsetStorage := make([]byte, 0)

	storage := NewMockSyncOffsetStorage(ctrl)
	storage.EXPECT().SaveSyncOffset(gomock.Any()).DoAndReturn(func(offset *SyncOffset) error {
		syncOffsetStorage = offset.Bytes()
		return nil
	}).AnyTimes()
	storage.EXPECT().GetSyncOffset().DoAndReturn(func() (*SyncOffset, bool, error) {
		if len(syncOffsetStorage) == 0 {
			return nil, false, nil
		}
		offset := new(SyncOffset)
		offset.SetBytes(syncOffsetStorage)
		return offset, true, nil
	}).AnyTimes()
	return storage
}
