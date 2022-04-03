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
	MsgID() MsgIDFunc
	WithScoring() bool
}

type encoding interface {
	EncodeNetworkMsg(msg message.Encoder) ([]byte, error)
	DecodeNetworkMsg(data []byte) (message.Encoder, error)
}

// MsgIDFunc is the function that maps a message to a msg_id
type MsgIDFunc func(msg []byte) string
