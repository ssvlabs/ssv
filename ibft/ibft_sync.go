package ibft

import (
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
)

func (i *ibftImpl) ProcessSyncMessage(msg *network.SyncChanObj) {
	s := sync.NewReqHandler(i.logger, msg.Msg.ValidatorPk, i.network, i.ibftStorage)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and sync to it.
func (i *ibftImpl) SyncIBFT() {
	s := sync.NewHistorySync(i.logger, i.ValidatorShare.ValidatorPK.Serialize(), i.network, i.ibftStorage, i.validateDecidedMsg)
	s.Start()
}
