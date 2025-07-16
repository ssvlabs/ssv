package networkconfig

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

const (
	AlanFork            ForkName = iota
	NetworkTopologyFork          // TODO: rename
)

type ForkName int // numeric type for comparison

func (f ForkName) String() string {
	switch f {
	case AlanFork:
		return "alan"
	case NetworkTopologyFork:
		return "network_topology" // TODO: rename
	default:
		return "unknown"
	}
}

type SSVFork struct {
	Name  ForkName
	Epoch phase0.Epoch
}
