package forks

import (
	"github.com/bloxapp/ssv/protocol/v1"
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
	EncodeNetworkMsg(msg v1.MessageEncoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (v1.MessageEncoder, error)
}
