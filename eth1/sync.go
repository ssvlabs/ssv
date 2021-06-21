package eth1

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
	"sync"
)

const (
	defaultSyncOffset string = "49e08f"
)

// SyncOffset is the type of variable used for passing around the offset
type SyncOffset = big.Int

// SyncOffsetStorage represents the interface for compatible storage
type SyncOffsetStorage interface {
	// SaveSyncOffset saves the offset (block number)
	SaveSyncOffset(offset *SyncOffset) error
	// GetSyncOffset returns the sync offset
	GetSyncOffset() (*SyncOffset, error)
}

// DefaultSyncOffset returns the default value (block number of the first event from the contract)
func DefaultSyncOffset() *SyncOffset {
	return NewSyncOffset(defaultSyncOffset)
}

// NewSyncOffset returns the default value (block number of the first event from the contract)
func NewSyncOffset(shex string) *SyncOffset {
	if len(shex) == 0 {
		return nil
	}
	offset := new(SyncOffset)
	offset.SetString(shex, 16)
	return offset
}

// SyncEth1Events sync past events
func SyncEth1Events(logger *zap.Logger, client Client, storage SyncOffsetStorage, observerID string, syncOffset *SyncOffset) error {
	logger.Info("syncing eth1 contract events")

	cn, err := client.EventsSubject().Register(observerID)
	if err != nil {
		return errors.Wrap(err, "failed to register on contract events subject")
	}
	defer client.EventsSubject().Deregister(observerID)

	// listen on the given channel
	// will stop once SyncEndedEvent arrives
	var syncEndedEvent SyncEndedEvent
	var syncWg sync.WaitGroup
	syncWg.Add(1)
	go func() {
		defer syncWg.Done()
		for e := range cn {
			if event, ok := e.(Event); ok {
				if syncEndedEvent, ok = event.Data.(SyncEndedEvent); ok {
					return
				}
				logger.Debug("got new event from eth1 sync", zap.Uint64("BlockNumber", event.Log.BlockNumber))
			}
		}
	}()
	// calling eth1 client to sync data, sync offset value is one of (by priority):
	//   1. provided value (config)
	//   2. last sync offset
	//   3. default sync offset (the genesis block of the contract)
	if syncOffset == nil {
		syncOffset, err = storage.GetSyncOffset()
		if err != nil {
			logger.Debug("could not get sync offset for eth1 sync, using default offset",
				zap.String("defaultSyncOffset", defaultSyncOffset))
			syncOffset = DefaultSyncOffset()
		}
	}
	err = client.Sync(syncOffset)
	if err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	// waiting for eth1 sync to finish
	syncWg.Wait()

	return upgradeSyncOffset(logger, storage, syncOffset, syncEndedEvent)
}

// upgradeSyncOffset updates the sync offset after a sync
func upgradeSyncOffset(logger *zap.Logger, storage SyncOffsetStorage, syncOffset *SyncOffset, syncEndedEvent SyncEndedEvent) error {
	nResults := len(syncEndedEvent.Logs)
	if nResults > 0 {
		if !syncEndedEvent.Success {
			logger.Warn("could not parse all events from eth1, sync offset will not be upgraded")
		} else if rawOffset := syncEndedEvent.Logs[nResults-1].BlockNumber; rawOffset > syncOffset.Uint64() {
			logger.Debug("upgrading sync offset", zap.Uint64("syncOffset", rawOffset))
			syncOffset.SetUint64(rawOffset)
			if err := storage.SaveSyncOffset(syncOffset); err != nil {
				return errors.Wrap(err, "could not upgrade sync offset")
			}
		}
	}
	return nil
}
