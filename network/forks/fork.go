package forks

import "github.com/bloxapp/ssv/network"

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
}

type encoding interface {
	EncodeNetworkMsg(msg *network.Message) ([]byte, error)
	DecodeNetworkMsg(data []byte) (*network.Message, error)
}
