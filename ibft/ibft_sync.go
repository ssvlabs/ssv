package ibft

import (
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
)

func (i *ibftImpl) ProcessSyncMessage(msg *network.SyncChanObj) {
	s := sync.NewReqHandler(i.logger, msg.Msg.ValidatorPk, i.network, i.ibftStorage)
	go s.Process(msg)
}
