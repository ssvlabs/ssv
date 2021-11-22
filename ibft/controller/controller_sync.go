package controller

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/ibft/sync/incoming"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"
	"time"
)

const syncRetries = 3

// processSyncQueueMessages is listen for all the ibft sync msg's and process them
func (i *Controller) processSyncQueueMessages() {
	go func() {
		for {
			if syncMsg := i.msgQueue.PopMessage(msgqueue.SyncIndexKey(i.Identifier)); syncMsg != nil {
				i.ProcessSyncMessage(&network.SyncChanObj{
					Msg:    syncMsg.SyncMessage,
					Stream: syncMsg.Stream,
				})
			}
			time.Sleep(time.Millisecond * 100)
		}
	}()
	i.logger.Info("sync messages queue started")
}

// ProcessSyncMessage - processes sync messages
func (i *Controller) ProcessSyncMessage(msg *network.SyncChanObj) {
	var lastChangeRoundMsg *proto.SignedMessage
	currentInstaceSeqNumber := int64(-1)
	if i.currentInstance != nil {
		lastChangeRoundMsg = i.currentInstance.GetLastChangeRoundMsg()
		currentInstaceSeqNumber = int64(i.currentInstance.State().SeqNumber.Get())
	}
	s := incoming.New(i.logger, i.Identifier, currentInstaceSeqNumber, i.network, i.ibftStorage, lastChangeRoundMsg)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *Controller) SyncIBFT() error {
	if !i.syncingLock.TryAcquire(1) {
		return errors.New("failed to start iBFT sync, already running")
	}
	defer i.syncingLock.Release(1)

	i.logger.Info("syncing iBFT..")

	// stop current instance and return any waiting chan.
	if i.currentInstance != nil {
		i.currentInstance.Stop()
	}

	return i.syncIBFT()
}

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
