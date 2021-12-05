package p2p

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"math/big"
	"sync"
	"time"
)

type listener struct {
	msgCh     chan *proto.SignedMessage
	sigCh     chan *proto.SignedMessage
	decidedCh chan *proto.SignedMessage
	syncCh    chan *network.SyncChanObj

	msgType network.NetworkMsg
	id      string
}

func (l *listener) MsgChan() chan *proto.SignedMessage {
	return l.msgCh
}

func (l *listener) SigChan() chan *proto.SignedMessage {
	return l.sigCh
}

func (l *listener) DecidedChan() chan *proto.SignedMessage {
	return l.decidedCh
}

func (l *listener) SyncChan() chan *network.SyncChanObj {
	return l.syncCh
}

func newListener(msgType network.NetworkMsg) *listener {
	switch msgType {
	case network.NetworkMsg_IBFTType:
		return &listener{
			msgCh:  make(chan *proto.SignedMessage, MsgChanSize),
			msgType: network.NetworkMsg_IBFTType,
		}
	case network.NetworkMsg_SignatureType:
		return &listener{
			sigCh:  make(chan *proto.SignedMessage, MsgChanSize),
			msgType: network.NetworkMsg_SignatureType,
		}
	case network.NetworkMsg_DecidedType:
		return &listener{
			decidedCh:  make(chan *proto.SignedMessage, MsgChanSize),
			msgType: network.NetworkMsg_DecidedType,
		}
	case network.NetworkMsg_SyncType:
		return &listener{
			syncCh:  make(chan *network.SyncChanObj, MsgChanSize),
			msgType: network.NetworkMsg_SyncType,
		}
	default:
		return nil
	}
}

type listeners map[network.NetworkMsg][]*listener

type listenersContainer struct {
	ctx       context.Context
	lock      *sync.RWMutex
	logger    *zap.Logger
	listeners listeners
	added     chan *listener
	removed   chan *listener
}

func newListenersContainer(ctx context.Context, logger *zap.Logger) *listenersContainer {
	return &listenersContainer{
		ctx:       ctx,
		lock:      &sync.RWMutex{},
		logger:    logger,
		listeners: make(listeners),
		added:     make(chan *listener, 1),
		removed:   make(chan *listener, 1),
	}
}

func (lc *listenersContainer) Start() {
	for {
		select {
		case l := <-lc.added:
			if l != nil {
				lc.addListener(l)
			}
		case l := <-lc.removed:
			if l != nil {
				lc.removeListener(l.msgType, l.id)
			}
		case <-lc.ctx.Done():
			return
		}
	}
}

func (lc *listenersContainer) register(l *listener) func() {
	lc.added <- l

	return func() {
		lc.removed <- l
	}
}

func (lc *listenersContainer) getListeners(msgType network.NetworkMsg) []*listener {
	lc.lock.RLock()
	defer lc.lock.RUnlock()

	lss, ok := lc.listeners[msgType]
	if !ok {
		return []*listener{}
	}
	res := make([]*listener, len(lss))
	copy(res, lss)
	return res
}

func (lc *listenersContainer) addListener(l *listener) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(int64(1000000)))
	if err != nil {
		lc.logger.Error("could not create random number for listener")
		return
	}
	lss, ok := lc.listeners[l.msgType]
	if !ok {
		lss = make([]*listener, 0)
	}
	id := fmt.Sprintf("%d:%d:%d", len(lss), time.Now().UnixNano(), r.Int64())
	l.id = id
	lss = append(lss, l)
	lc.listeners[l.msgType] = lss
}

func (lc *listenersContainer) removeListener(msgType network.NetworkMsg, id string) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	ls, ok := lc.listeners[msgType]
	if !ok {
		return
	}
	for i, l := range ls {
		if l.id == id {
			//n.ls[msgType] = append(ls[:i], ls[i+1:]...)
			// faster way of removing the item
			// it will mess up the order of the slice but it doesn't matter here
			ls[i] = ls[len(ls)-1]
			ls[len(ls)-1] = nil
			ls = ls[:len(ls)-1]
			lc.listeners[msgType] = ls
			return
		}
	}
}

// propagateSignedMsg takes an incoming message (from validator's topic)
// and propagates it to the corresponding internal listeners
func (n *p2pNetwork) propagateSignedMsg(cm *network.Message) {
	if cm == nil || cm.SignedMessage == nil {
		n.logger.Debug("could not propagate nil message")
		return
	}

	lss := n.listenersContainer.getListeners(cm.Type)

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

func propagateIBFTMessage(listeners []*listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.msgCh != nil {
			ls.msgCh <- msg
		}
	}
}

func propagateSigMessage(listeners []*listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.sigCh != nil {
			ls.sigCh <- msg
		}
	}
}

func propagateDecidedMessage(listeners []*listener, msg *proto.SignedMessage) {
	for _, ls := range listeners {
		if ls.decidedCh != nil {
			ls.decidedCh <- msg
		}
	}
}

func propagateSyncMessage(listeners []*listener, cm *network.Message, netSyncStream network.SyncStream) {
	for _, ls := range listeners {
		if ls.syncCh != nil {
			ls.syncCh <- &network.SyncChanObj{
				Msg:    cm.SyncMessage,
				Stream: netSyncStream,
			}
		}
	}
}
