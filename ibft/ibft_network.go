package ibft

import "github.com/bloxapp/ssv/network"

func (i *ibftImpl) listenToNetworkMessages() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for msg := range msgChan {
			i.msgQueue.AddMessage(&network.Message{
				Lambda:        msg.Message.Lambda,
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
	}()

	// decided messages
	decidedChan := i.network.ReceivedDecidedChan()
	go func() {
		for msg := range decidedChan {
			i.ProcessDecidedMessage(msg)
		}
	}()

	// sync messages
	syncChan := i.network.ReceivedSyncMsgChan()
	go func() {
		for msg := range syncChan {
			i.ProcessSyncMessage(msg)
		}
	}()
}
