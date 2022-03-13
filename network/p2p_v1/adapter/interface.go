package adapter

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
)

type Adapter interface {
	network.Network
	Setup() error
	Start() error
	Close() error

	Listeners() listeners.Container
}
