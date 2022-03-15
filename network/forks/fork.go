package forks

import (
	"github.com/bloxapp/ssv/protocol"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	pubSubMapping
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) string
}

type encoding interface {
	EncodeNetworkMsg(msg protocol.MessageEncoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (protocol.MessageEncoder, error)
}
