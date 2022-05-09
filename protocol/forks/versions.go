package forksprotocol

import (
	"math"

	types "github.com/prysmaticlabs/eth2-types"
)

// ForkVersion represents a fork version
type ForkVersion string

func (fv ForkVersion) String() string {
	return string(fv)
}

const (
	// V0ForkVersion is the version for v0
	V0ForkVersion ForkVersion = "v0"
	// V1ForkVersion is the version for v1
	V1ForkVersion ForkVersion = "v1"
)

var (
	// v1ForkEpoch is the epoch for fork version 0
	// TODO: set actual epoch when decided
	v1ForkEpoch = types.Epoch(math.MaxUint64)
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

// SetForkEpoch injects a custom epoch for a specific version
//
// NOTE: this is used for testing, in real scenario we won't allow using this to avoid conflicts
func SetForkEpoch(targetEpoch types.Epoch, forkVersion ForkVersion) {
	switch forkVersion {
	case V1ForkVersion:
		v1ForkEpoch = targetEpoch
	}
}
