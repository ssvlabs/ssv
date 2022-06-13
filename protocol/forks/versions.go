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
	// ForkVersionEmpty represents an empty version
	ForkVersionEmpty ForkVersion = ""
	// V0ForkVersion is the version for v0
	V0ForkVersion ForkVersion = "v0"
	// V1ForkVersion is the version for v1
	V1ForkVersion ForkVersion = "v1"
	// V2ForkVersion is the version for v2
	V2ForkVersion ForkVersion = "v2"
)

var (
	// v1ForkEpoch is the epoch for fork version 0
	// TODO: set actual epoch when decided
	v1ForkEpoch = types.Epoch(math.MaxUint64)

	// v2ForkEpoch is the epoch for fork version 1
	// TODO: set actual epoch when decided
	v2ForkEpoch = types.Epoch(math.MaxUint64)
)

// ForkHandler handles a fork event
type ForkHandler interface {
	// OnFork is called upon a ForkVersion change
	OnFork(forkVersion ForkVersion) error
}

// GetCurrentForkVersion returns the current fork version
func GetCurrentForkVersion(currentEpoch types.Epoch) ForkVersion {
	switch epoch := currentEpoch; {
	case epoch >= v2ForkEpoch: // check highest first
		return V2ForkVersion
	case epoch >= v1ForkEpoch:
		return V1ForkVersion
	default:
		return V0ForkVersion
	}
}

// SetForkEpoch injects a custom epoch for a specific version
//
// NOTE: this is used for testing, in real scenario we won't allow using this to avoid conflicts
func SetForkEpoch(targetEpoch types.Epoch, forkVersion ForkVersion) {
	switch forkVersion {
	case V1ForkVersion:
		v1ForkEpoch = targetEpoch
	case V2ForkVersion:
		v2ForkEpoch = targetEpoch
	}
}
