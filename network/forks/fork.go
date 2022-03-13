package forks

import "github.com/bloxapp/ssv/network"

type OnFork func()

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	SlotTick(slot uint64)
	IsForked() bool
	SetHandler(OnFork)
	pubSubMapping
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) string
}

type encoding interface {
	EncodeNetworkMsg(msg *network.Message) ([]byte, error)
	DecodeNetworkMsg(data []byte) (*network.Message, error)
}
