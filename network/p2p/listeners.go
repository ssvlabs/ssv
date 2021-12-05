package p2p

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"go.uber.org/zap"
)

// propagateSignedMsg takes an incoming message (from validator's topic)
// and propagates it to the corresponding internal listeners
func (n *p2pNetwork) propagateSignedMsg(cm *network.Message) {
	if cm == nil || cm.SignedMessage == nil {
		n.logger.Debug("could not propagate nil message")
		return
	}

	lss := n.listeners.GetListeners(cm.Type)

	switch cm.Type {
	case network.NetworkMsg_IBFTType:
		go propagateIBFTMessage(lss, cm.SignedMessage)
	case network.NetworkMsg_SignatureType:
		go propagateSigMessage(lss, cm.SignedMessage)
	case network.NetworkMsg_DecidedType:
		go propagateDecidedMessage(lss, cm.SignedMessage)
	default:
		n.logger.Error("received unsupported message", zap.Int32("msg type", int32(cm.Type)))
	}
}

func propagateIBFTMessage(listeners []*listeners.Listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		cn := ls.MsgChan()
		if cn != nil {
			cn <- msg
		}
	}
}

func propagateSigMessage(listeners []*listeners.Listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		cn := ls.SigChan()
		if cn != nil {
			cn <- msg
		}
	}
}

func propagateDecidedMessage(listeners []*listeners.Listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		cn := ls.DecidedChan()
		if cn != nil {
			cn <- msg
		}
	}
}

func propagateSyncMessage(listeners []*listeners.Listener, cm *network.Message, netSyncStream network.SyncStream) {
	for _, ls := range listeners {
		cn := ls.SyncChan()
		if cn != nil {
			cn <- &network.SyncChanObj{
				Msg:    cm.SyncMessage,
				Stream: netSyncStream,
			}
		}
	}
}
