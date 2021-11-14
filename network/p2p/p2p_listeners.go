package p2p

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"time"
)

type listener struct {
	msgCh     chan *proto.SignedMessage
	sigCh     chan *proto.SignedMessage
	decidedCh chan *proto.SignedMessage
	syncCh    chan *network.SyncChanObj
}

func (n *p2pNetwork) registerListener(ls listener) func() {
	n.listenersLock.Lock()
	defer n.listenersLock.Unlock()

	id := fmt.Sprintf("%d:%d", len(n.listenersMap), time.Now().UnixNano())
	n.listenersMap[id] = ls

	return func() {
		n.listenersLock.Lock()
		defer n.listenersLock.Unlock()
		delete(n.listenersMap, id)
	}
}

// propagateSignedMsg takes an incoming message (from validator's topic)
// and propagates it to the corresponding internal listeners
func (n *p2pNetwork) propagateSignedMsg(cm *network.Message) {
	if cm == nil || cm.SignedMessage == nil {
		n.logger.Debug("could not propagate nil message")
		return
	}
	n.logger.Debug("propagating msg to internal listeners", zap.String("type", cm.Type.String()),
		zap.Any("msg", cm.SignedMessage))

	// copy listeners with lock, to avoid data races
	n.listenersLock.Lock()
	var listeners []listener
	for _, ls := range n.listenersMap {
		listeners = append(listeners, ls)
	}
	n.listenersLock.Unlock()

	switch cm.Type {
	case network.NetworkMsg_IBFTType:
		go propagateIBFTMessage(listeners, cm.SignedMessage)
	case network.NetworkMsg_SignatureType:
		go propagateSigMessage(listeners, cm.SignedMessage)
	case network.NetworkMsg_DecidedType:
		go propagateDecidedMessage(listeners, cm.SignedMessage)
	default:
		n.logger.Error("received unsupported message", zap.Int32("msg type", int32(cm.Type)))
	}
}

func propagateIBFTMessage(listeners []listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.msgCh != nil {
			ls.msgCh <- msg
		}
	}
}

func propagateSigMessage(listeners []listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.sigCh != nil {
			ls.sigCh <- msg
		}
	}
}

func propagateDecidedMessage(listeners []listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.decidedCh != nil {
			ls.decidedCh <- msg
		}
	}
}
