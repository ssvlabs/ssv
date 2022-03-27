package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/core"
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
	EncodeNetworkMsg(msg core.MessageEncoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (core.MessageEncoder, error)
}
