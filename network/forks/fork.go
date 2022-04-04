package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/protocol"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	pubSubMapping
	pubSubConfig
	sync
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) []string
}

type pubSubConfig interface {
	MsgID() MsgIDFunc
}

type encoding interface {
	EncodeNetworkMsg(msg message.Encoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (message.Encoder, error)
}

type sync interface {
	// LastDecidedProtocol returns the protocol id of last decided protocol,
	// and the amount of peers for distribution
	LastDecidedProtocol() (protocol.ID, int)
	// LastChangeRoundProtocol returns the protocol id of last change round protocol,
	// and the amount of peers for distribution
	LastChangeRoundProtocol() (protocol.ID, int)
	// DecidedHistoryProtocol returns the protocol id of decided history protocol,
	// and the amount of peers for distribution
	DecidedHistoryProtocol() (protocol.ID, int)
}

// MsgIDFunc is the function that maps a message to a msg_id
type MsgIDFunc func(msg []byte) string
