package node

import (
	"github.com/bloxapp/ssv/eth1"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

const (
	defaultSyncOffset string = "49e08f"
)

// startEth1 starts to sync and listen to events
func (n *ssvNode) startEth1() {
	// setup validator controller to listen to ValidatorAdded events
	// this will handle events from the sync as well
	cnValidators, err := n.eth1Client.EventsSubject().Register("ValidatorControllerObserver")
	if err != nil {
		n.logger.Error("failed to register on contract events subject", zap.Error(err))
	}
	go n.validatorController.ListenToEth1Events(cnValidators)

	n.logger.Debug("syncing eth1 events")
	// sync past events
	if err := n.syncEth1(); err != nil {
		n.logger.Error("failed to sync eth1 events:", zap.Error(err))
	} else {
		n.logger.Info("manage to sync events from eth1")
	}

	n.logger.Debug("starting eth1 events subscription")
	// starts the eth1 events subscription
	err = n.eth1Client.Start()
	if err != nil {
		n.logger.Error("failed to start eth1 client", zap.Error(err))
	}
}

// syncEth1 sync past events
func (n *ssvNode) syncEth1() error {
	cn, err := n.eth1Client.EventsSubject().Register("SSVNodeObserver")
	if err != nil {
		return errors.Wrap(err, "failed to register on contract events subject")
	}
	// listen on the given channel
	// will stop once SyncEndedEvent arrives
	var syncEndedEvent eth1.SyncEndedEvent
	var syncWg sync.WaitGroup
	syncWg.Add(1)
	go func() {
		defer syncWg.Done()
		for e := range cn {
			if event, ok := e.(eth1.Event); ok {
				n.logger.Debug("got new event from eth1 sync", zap.Uint64("BlockNumber", event.Log.BlockNumber))
				if syncEndedEvent, ok = event.Data.(eth1.SyncEndedEvent); ok {
					return
				}
			}
		}
	}()
	// calling eth1 client to sync data from the last sync offset (or new if not exist)
	syncOffset, err := n.storage.GetSyncOffset()
	if err != nil {
		n.logger.Debug("could not get sync offset, using default offset")
		syncOffset = DefaultSyncOffset()
	}
	err = n.eth1Client.Sync(syncOffset)
	if err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	// waiting for eth1 sync to finish
	syncWg.Wait()

	return n.upgradeSyncOffset(syncOffset, syncEndedEvent)
}

// upgradeSyncOffset updates the sync offset after a sync
func (n *ssvNode) upgradeSyncOffset(syncOffset *SyncOffset, syncEndedEvent eth1.SyncEndedEvent) error {
	nResults := len(syncEndedEvent.Logs)
	if nResults > 0 {
		if !syncEndedEvent.Success {
			n.logger.Warn("could not parse all events from eth1, sync offset will not be upgraded")
		} else if rawOffset := syncEndedEvent.Logs[nResults-1].BlockNumber; rawOffset > syncOffset.Uint64() {
			n.logger.Debug("upgrading sync offset", zap.Uint64("offset", rawOffset))
			syncOffset.SetUint64(rawOffset)
			if err := n.storage.SaveSyncOffset(syncOffset); err != nil {
				return errors.Wrap(err, "could not upgrade sync offset")
			}
		}
	}
	return nil
}

// DefaultSyncOffset returns the default value (block number of the first event from the contract)
func DefaultSyncOffset() *SyncOffset {
	offset := new(SyncOffset)
	offset.SetString(defaultSyncOffset, 16)
	return offset
}
