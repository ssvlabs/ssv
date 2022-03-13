package adapter

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
)

// Adapter is wrapping v1 components and implementation with a v0 interface
type Adapter interface {
	network.Network
	Setup() error
	Start() error
	Close() error
	// Listeners returns the list of active listeners
	Listeners() listeners.Container
}
