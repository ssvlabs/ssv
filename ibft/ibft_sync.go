package ibft

import (
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

func (i *ibftImpl) ProcessSyncMessage(msg *network.SyncChanObj) {
	s := sync.NewReqHandler(i.logger, msg.Msg.ValidatorPk, i.network, i.ibftStorage)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *ibftImpl) SyncIBFT() {
	s := sync.NewHistorySync(i.logger, i.ValidatorShare.PublicKey.Serialize(), i.network, i.ibftStorage, i.validateDecidedMsg)
	err := s.Start()
	if err != nil {
		i.logger.Error("history sync failed", zap.Error(err))
	}
}
