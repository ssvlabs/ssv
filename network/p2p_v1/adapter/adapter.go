package adapter

import "github.com/bloxapp/ssv/network"

type Adapter interface {
	network.Network
	Setup() error
	Start() error
	Close() error
}
