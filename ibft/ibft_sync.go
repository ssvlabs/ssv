package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/ibft/sync/incoming"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
	"time"
)

// processSyncQueueMessages is listen for all the ibft sync msg's and process them
func (i *ibftImpl) processSyncQueueMessages() {
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
}

func (i *ibftImpl) ProcessSyncMessage(msg *network.SyncChanObj) {
	var lastChangeRoundMsg *proto.SignedMessage
	currentInstaceSeqNumber := int64(-1)
	if i.currentInstance != nil {
		lastChangeRoundMsg = i.currentInstance.GetLastChangeRoundMsg()
		currentInstaceSeqNumber = int64(i.currentInstance.State.SeqNumber.Get())
	}
	s := incoming.New(i.logger, i.Identifier, currentInstaceSeqNumber, i.network, i.ibftStorage, lastChangeRoundMsg)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *ibftImpl) SyncIBFT() {
	i.logger.Info("syncing iBFT..")

	// stop current instance and return any waiting chan.
	if i.currentInstance != nil {
		i.currentInstance.Stop()
	}

	// sync
	s := history.New(i.logger, i.ValidatorShare.PublicKey.Serialize(), i.GetIdentifier(), i.network, i.ibftStorage, i.validateDecidedMsg)
	err := s.Start()
	if err != nil {
		i.logger.Fatal("history sync failed", zap.Error(err))
	}
}
