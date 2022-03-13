package controller

import (
	"context"
	"time"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/ibft/sync/incoming"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"
)

// syncRetries is the number of reties to perform for history sync
const syncRetries = 3

// processSyncQueueMessages is listen for all the ibft sync msg's and process them
func (i *Controller) processSyncQueueMessages() {
	go func() {
		for {
			if syncMsg := i.msgQueue.PopMessage(msgqueue.SyncIndexKey(i.Identifier)); syncMsg != nil {
				i.ProcessSyncMessage(&network.SyncChanObj{
					Msg:      syncMsg.SyncMessage,
					StreamID: syncMsg.StreamID,
				})
			}
			time.Sleep(i.syncRateLimit)
		}
	}()
	i.logger.Info("sync messages queue started")
}

// ProcessSyncMessage processes sync messages
func (i *Controller) ProcessSyncMessage(msg *network.SyncChanObj) {
	i.logger.Info("ProcessSyncMessage: processing message")
	var lastChangeRoundMsg *proto.SignedMessage
	currentInstanceSeqNumber := int64(-1)
	if i.currentInstance != nil && i.currentInstance.State() != nil && i.currentInstance.State().SeqNumber != nil {
		lastChangeRoundMsg = i.currentInstance.GetLastChangeRoundMsg()
		currentInstanceSeqNumber = int64(i.currentInstance.State().SeqNumber.Get())
	}
	s := incoming.New(i.logger, i.Identifier, currentInstanceSeqNumber, i.network, i.ibftStorage, lastChangeRoundMsg)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
// it will ensure that minimum peers are available on the validator's topic
func (i *Controller) SyncIBFT() error {
	if !i.syncingLock.TryAcquire(1) {
		return ErrAlreadyRunning
	}
	defer i.syncingLock.Release(1)

	i.logger.Info("syncing iBFT..")

	// stop current instance and return any waiting chan.
	if i.currentInstance != nil {
		i.currentInstance.Stop()
	}

	err := i.syncIBFT()
	if err != nil {
		return err
	}
	i.initSynced.Set(true)
	return nil
}

// syncIBFT takes care of history sync by retrying sync, it is non thread-safe
// and shouldn't be called directly, use SyncIBFT
func (i *Controller) syncIBFT() error {
	// TODO: use controller context once added
	return tasks.RetryWithContext(context.Background(), func() error {
		i.waitForMinPeerOnInit(1)
		s := history.New(i.logger, i.ValidatorShare.PublicKey.Serialize(), i.ValidatorShare.CommitteeSize(),
			i.GetIdentifier(), i.network, i.ibftStorage, i.ValidateDecidedMsg)
		err := s.Start()
		if err != nil {
			return errors.Wrap(err, "history sync failed")
		}
		return nil
	}, syncRetries)
}
