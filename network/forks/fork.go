package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/protocol"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	pubSubMapping
	pubSubConfig
	sync
	nodeRecord
}

type nodeRecord interface {
	// DecorateNode decorates the given node's record
	DecorateNode(node *enode.LocalNode, args map[string]interface{}) error
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) []string
}

type pubSubConfig interface {
	MsgID() MsgIDFunc
}

type encoding interface {
	EncodeNetworkMsg(msg *message.SSVMessage) ([]byte, error)
	DecodeNetworkMsg(data []byte) (*message.SSVMessage, error)
}

type sync interface {
	// ProtocolID returns the protocol id of given protocol,
	// and the amount of peers for distribution
	ProtocolID(prot p2pprotocol.SyncProtocol) (protocol.ID, int)
}

// MsgIDFunc is the function that maps a message to a msg_id
type MsgIDFunc func(msg []byte) string
