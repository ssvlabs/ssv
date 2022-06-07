package eth1

import (
	"github.com/bloxapp/ssv/eth1/abiparser"
	"math/big"
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:generate mockgen -package=eth1 -destination=./mock_sync.go -source=./sync.go

const (
	// prod contract genesis
	defaultSyncOffset string = "4e706f"
	// stage contract genesis -> 49e08f
)

// SyncOffset is the type of variable used for passing around the offset
type SyncOffset = big.Int

// SyncEventHandler handles a given event
type SyncEventHandler func(Event) error

// SyncOffsetStorage represents the interface for compatible storage
type SyncOffsetStorage interface {
	// SaveSyncOffset saves the offset (block number)
	SaveSyncOffset(offset *SyncOffset) error
	// GetSyncOffset returns the sync offset
	GetSyncOffset() (*SyncOffset, bool, error)
}

// DefaultSyncOffset returns the default value (block number of the first event from the contract)
func DefaultSyncOffset() *SyncOffset {
	return HexStringToSyncOffset(defaultSyncOffset)
}

// HexStringToSyncOffset converts an hex string to SyncOffset
func HexStringToSyncOffset(shex string) *SyncOffset {
	if len(shex) == 0 {
		return nil
	}
	offset := new(SyncOffset)
	offset.SetString(shex, 16)
	return offset
}

// SyncEth1Events sync past events
func SyncEth1Events(logger *zap.Logger, client Client, storage SyncOffsetStorage, syncOffset *SyncOffset, handler SyncEventHandler) error {
	logger.Info("syncing eth1 contract events")

	cn := make(chan *Event, 4096)
	feed := client.EventsFeed()
	sub := feed.Subscribe(cn)

	// Stop once SyncEndedEvent arrives
	var errs []error
	var syncEndedEvent SyncEndedEvent
	var syncWg sync.WaitGroup
	syncWg.Add(1)
	go func() {
		var ok bool
		defer syncWg.Done()
		defer sub.Unsubscribe()
		for event := range cn {
			if syncEndedEvent, ok = event.Data.(SyncEndedEvent); ok {
				return
			}
			if handler != nil {
				err := handler(*event)
				var decryptErr *abiparser.DecryptError
				if err != nil && !errors.As(err, &decryptErr) {
					errs = append(errs, err)
				}
			}
		}
	}()
	syncOffset = determineSyncOffset(logger, storage, syncOffset)
	if err := client.Sync(syncOffset); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	// waiting for eth1 sync to finish
	syncWg.Wait()

	if len(errs) > 0 {
		logger.Error("failed to handle all events from sync", zap.Any("errs", errs))
		return errors.New("failed to handle all events from sync")
	}

	return upgradeSyncOffset(logger, storage, syncOffset, syncEndedEvent)
}

// upgradeSyncOffset updates the sync offset after a sync
func upgradeSyncOffset(logger *zap.Logger, storage SyncOffsetStorage, syncOffset *SyncOffset, syncEndedEvent SyncEndedEvent) error {
	nResults := len(syncEndedEvent.Logs)
	if nResults > 0 {
		if !syncEndedEvent.Success {
			logger.Warn("could not parse all events from eth1")
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

// determineSyncOffset decides what is the value of sync offset by using one of (by priority):
//   1. last saved sync offset
//   2. provided value (from config)
//   3. default sync offset (the genesis block of the contract)
func determineSyncOffset(logger *zap.Logger, storage SyncOffsetStorage, syncOffset *SyncOffset) *SyncOffset {
	syncOffsetFromStorage, found, err := storage.GetSyncOffset()
	if err != nil {
		logger.Warn("failed to get sync offset", zap.Error(err))
	}
	if found && syncOffsetFromStorage != nil {
		logger.Debug("using last sync offset",
			zap.Uint64("syncOffset", syncOffsetFromStorage.Uint64()))
		return syncOffsetFromStorage
	}
	if syncOffset != nil { // if provided sync offset is nil - use default sync offset
		logger.Debug("using provided sync offset",
			zap.Uint64("syncOffset", syncOffset.Uint64()))
		return syncOffset
	}
	syncOffset = DefaultSyncOffset()
	logger.Debug("using default sync offset",
		zap.Uint64("syncOffset", syncOffset.Uint64()))
	return syncOffset
}
