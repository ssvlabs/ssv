package listeners

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

const (
	// MsgChanSize is the buffer size of the message channel
	MsgChanSize = 128
)

// Listener represents an internal listener
type Listener struct {
	msgCh     chan *proto.SignedMessage
	sigCh     chan *proto.SignedMessage
	decidedCh chan *proto.SignedMessage
	syncCh    chan *network.SyncChanObj

	msgType network.NetworkMsg
	id      string
}

// MsgChan returns the underlying msg channel
func (l *Listener) MsgChan() chan *proto.SignedMessage {
	return l.msgCh
}

// SigChan returns the underlying signature channel
func (l *Listener) SigChan() chan *proto.SignedMessage {
	return l.sigCh
}

// DecidedChan returns the underlying decided channel
func (l *Listener) DecidedChan() chan *proto.SignedMessage {
	return l.decidedCh
}

// SyncChan returns the underlying sync channel
func (l *Listener) SyncChan() chan *network.SyncChanObj {
	return l.syncCh
}

// NewListener creates a new instance of listener
func NewListener(msgType network.NetworkMsg) *Listener {
	switch msgType {
	case network.NetworkMsg_IBFTType:
		return &Listener{
			msgCh:   make(chan *proto.SignedMessage, MsgChanSize),
			msgType: network.NetworkMsg_IBFTType,
		}
	case network.NetworkMsg_SignatureType:
		return &Listener{
			sigCh:   make(chan *proto.SignedMessage, MsgChanSize),
			msgType: network.NetworkMsg_SignatureType,
		}
	case network.NetworkMsg_DecidedType:
		return &Listener{
			decidedCh: make(chan *proto.SignedMessage, MsgChanSize),
			msgType:   network.NetworkMsg_DecidedType,
		}
	case network.NetworkMsg_SyncType:
		return &Listener{
			syncCh:  make(chan *network.SyncChanObj, MsgChanSize),
			msgType: network.NetworkMsg_SyncType,
		}
	default:
		return nil
	}
}
