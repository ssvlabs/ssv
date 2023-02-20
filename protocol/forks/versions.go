package forksprotocol

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

// ForkVersion represents a fork version
type ForkVersion string

func (fv ForkVersion) String() string {
	return string(fv)
}

const (
	// ForkVersionEmpty represents an empty version
	ForkVersionEmpty ForkVersion = ""
	// GenesisForkVersion is the version for v0
	GenesisForkVersion ForkVersion = "genesis"
)

// ForkHandler handles a fork event
type ForkHandler interface {
	// OnFork is called upon a ForkVersion change
	OnFork(logger *zap.Logger, forkVersion ForkVersion) error
}

// GetCurrentForkVersion returns the current fork version
func GetCurrentForkVersion(currentEpoch phase0.Epoch) ForkVersion {
	return GenesisForkVersion
}
