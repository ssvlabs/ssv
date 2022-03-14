package forks

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	encodingV1
	SlotTick(slot uint64)
	pubSubMapping
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) string
}

// TODO: remove v0
type encoding interface {
	EncodeNetworkMsg(msg *network.Message) ([]byte, error)
	DecodeNetworkMsg(data []byte) (*network.Message, error)
}
type encodingV1 interface {
	EncodeNetworkMsgV1(msg *protocol.SSVMessage) ([]byte, error)
	DecodeNetworkMsgV1(data []byte) (*protocol.SSVMessage, error)
}
