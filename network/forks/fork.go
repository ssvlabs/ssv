package forks

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	pubSubMapping
	pubSubConfig
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) []string
}

type pubSubConfig interface {
	WithMsgID() bool
	WithScoring() bool
}

type encoding interface {
	EncodeNetworkMsg(msg message.Encoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (message.Encoder, error)
}
