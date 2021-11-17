package p2p

import (
	"crypto/rand"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"math/big"
	"time"
)

type listener struct {
	msgCh     chan *proto.SignedMessage
	sigCh     chan *proto.SignedMessage
	decidedCh chan *proto.SignedMessage
	syncCh    chan *network.SyncChanObj
}

// registerListener registers the given listener
func (n *p2pNetwork) registerListener(ls listener) func() {
	r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(int64(1000000)))
	if err != nil {
		n.logger.Error("could not create random number for listener")
	}

	n.listenersLock.Lock()
	defer n.listenersLock.Unlock()

	id := fmt.Sprintf("%d:%d:%d", len(n.listeners), time.Now().UnixNano(), r.Int64())
	n.listeners[id] = ls

	return func() {
		n.listenersLock.Lock()
		defer n.listenersLock.Unlock()

		delete(n.listeners, id)
	}
}

// propagateSignedMsg takes an incoming message (from validator's topic)
// and propagates it to the corresponding internal listeners
func (n *p2pNetwork) propagateSignedMsg(cm *network.Message) {
	if cm == nil || cm.SignedMessage == nil {
		n.logger.Debug("could not propagate nil message")
		return
	}

	listeners := n.getListeners()

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

// getListeners copies listeners to avoid data races when iterating over listeners
func (n *p2pNetwork) getListeners() []listener {
	n.listenersLock.RLock()
	defer n.listenersLock.RUnlock()

	var listeners []listener
	for _, ls := range n.listeners {
		listeners = append(listeners, ls)
	}
	return listeners
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

func propagateSyncMessage(listeners []listener, cm *network.Message, netSyncStream network.SyncStream) {
	for _, ls := range listeners {
		if ls.syncCh != nil {
			ls.syncCh <- &network.SyncChanObj{
				Msg:    cm.SyncMessage,
				Stream: netSyncStream,
			}
		}
	}
}
