package forksprotocol

import (
	types "github.com/prysmaticlabs/eth2-types"
	"math"
)

// ForkVersion represents a fork version
type ForkVersion string

const (
	// V0ForkVersion is the version for v0
	V0ForkVersion ForkVersion = "v0"
	// v1ForkEpoch is the epoch for fork version 0
	// TODO: set actual epoch when decided
	v1ForkEpoch = math.MaxUint64
	//v1ForkEpoch = 91366
	// V1ForkVersion is the version for v1
	V1ForkVersion ForkVersion = "v1"
)

// ForkHandler handles a fork event
type ForkHandler interface {
	// OnFork is called upon a ForkVersion change
	OnFork(forkVersion ForkVersion) error
}

// GetCurrentForkVersion returns the current fork version
func GetCurrentForkVersion(currentEpoch types.Epoch) ForkVersion {
	if currentEpoch >= v1ForkEpoch {
		return V1ForkVersion
	}
	return V0ForkVersion
}
