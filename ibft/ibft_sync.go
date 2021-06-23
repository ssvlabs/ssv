package ibft

import (
	ibft_sync "github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
	"time"
)

// listenToSyncQueueMessages is listen for all the ibft sync msg's and process them
func (i *ibftImpl) listenToSyncQueueMessages(pubKey []byte) {
	go func() {
		for {
			if syncMsg := i.msgQueue.PopMessage(msgqueue.SyncIndexKey(pubKey)); syncMsg != nil {
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
	s := ibft_sync.NewReqHandler(i.logger, msg.Msg.ValidatorPk, i.network, i.ibftStorage)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *ibftImpl) SyncIBFT() {
	s := ibft_sync.NewHistorySync(i.logger, i.ValidatorShare.PublicKey.Serialize(), i.network, i.ibftStorage, i.validateDecidedMsg)
	err := s.Start()
	if err != nil {
		i.logger.Error("history sync failed", zap.Error(err))
	}
}
