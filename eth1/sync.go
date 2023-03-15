package eth1

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/bloxapp/ssv/logging/fields"

	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:generate mockgen -package=eth1 -destination=./mock_sync.go -source=./sync.go

const (
	// prod contract genesis
	defaultSyncOffset string = "5140591"
	// stage contract genesis -> 49e08f
)

// SyncOffset is the type of variable used for passing around the offset
type SyncOffset = big.Int

// SyncEventHandler handles a given event
type SyncEventHandler func(Event) ([]zap.Field, error)

// SyncOffsetStorage represents the interface for compatible storage
type SyncOffsetStorage interface {
	// SaveSyncOffset saves the offset (block number)
	SaveSyncOffset(offset *SyncOffset) error
	// GetSyncOffset returns the sync offset
	GetSyncOffset() (*SyncOffset, bool, error)
}

// DefaultSyncOffset returns the default value (block number of the first event from the contract)
func DefaultSyncOffset() *SyncOffset {
	return StringToSyncOffset(defaultSyncOffset)
}

// StringToSyncOffset converts string to SyncOffset
func StringToSyncOffset(syncOffset string) *SyncOffset {
	if len(syncOffset) == 0 {
		return nil
	}
	offset := new(SyncOffset)
	offset.SetString(syncOffset, 10)
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
				logFields, err := handler(*event)
				errs = HandleEventResult(logger, *event, logFields, err, false)
			}
		}
	}()
	syncOffset = determineSyncOffset(logger, storage, syncOffset)
	if err := client.Sync(logger, syncOffset); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	// waiting for eth1 sync to finish
	syncWg.Wait()

	if len(errs) > 0 {
		logger.Warn("could not handle some of the events during history sync", zap.Any("errs", errs))
		return errors.New("could not handle some of the events during history sync")
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
			logger.Info("upgrading sync offset", zap.Uint64("syncOffset", rawOffset))
			syncOffset.SetUint64(rawOffset)
			if err := storage.SaveSyncOffset(syncOffset); err != nil {
				return errors.Wrap(err, "could not upgrade sync offset")
			}
		}
	}
	return nil
}

// determineSyncOffset decides what is the value of sync offset by using one of (by priority):
//  1. last saved sync offset
//  2. provided value (from config)
//  3. default sync offset (the genesis block of the contract)
func determineSyncOffset(logger *zap.Logger, storage SyncOffsetStorage, syncOffset *SyncOffset) *SyncOffset {
	syncOffsetFromStorage, found, err := storage.GetSyncOffset()
	if err != nil {
		logger.Warn("failed to get sync offset", zap.Error(err))
	}
	if found && syncOffsetFromStorage != nil {
		logger.Debug("using last sync offset", fields.SyncOffset(syncOffsetFromStorage))
		return syncOffsetFromStorage
	}
	if syncOffset != nil { // if provided sync offset is nil - use default sync offset
		logger.Debug("using provided sync offset", fields.SyncOffset(syncOffset))
		return syncOffset
	}
	syncOffset = DefaultSyncOffset()
	logger.Debug("using default sync offset", fields.SyncOffset(syncOffset))
	return syncOffset
}

// HandleEventResult handles the result of an event
func HandleEventResult(logger *zap.Logger, event Event, logFields []zap.Field, err error, ongoingSync bool) []error {
	var errs []error
	syncTitle := "history"
	if ongoingSync {
		syncTitle = "ongoing"
	}

	if err != nil || len(logFields) > 0 {
		logger = logger.With(
			fields.EventName(event.Name),
			fields.BlockNumber(event.Log.BlockNumber),
			fields.TxHash(event.Log.TxHash),
		)
		if len(logFields) > 0 {
			logger = logger.With(logFields...)
		}
	}
	if err != nil {
		logger = logger.With(zap.Error(err))
		var malformedEventErr *abiparser.MalformedEventError

		if errors.As(err, &malformedEventErr) {
			logger.Warn(fmt.Sprintf("could not handle %s sync event, the event is malformed", syncTitle))
		} else {
			logger.Error(fmt.Sprintf("could not handle %s sync event", syncTitle))
			errs = append(errs, err)
		}
	} else if logger != nil {
		logger.Info(fmt.Sprintf("%s sync event was handled successfully", syncTitle))
	}

	return errs
}
