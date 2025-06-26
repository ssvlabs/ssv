package networkconfig

import (
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SSVForkName is a numerical identifier of specific SSV protocol forks.
type SSVForkName int

const (
	Alan              SSVForkName = iota
	GasLimit36M                   // Gas limit increase from 30M to 36M - upgrade from the default gas limit value of 30_000_000 to 36_000_000
	FinalityConsensus             // TODO: use a different name when we have a better one
)

// String implements fmt.Stringer.
func (f SSVForkName) String() string {
	s, ok := forkToString[f]
	if !ok {
		return "Unknown fork"
	}
	return s
}

var forkToString = map[SSVForkName]string{
	Alan:              "Alan",
	GasLimit36M:       "Gas Limit 36M",
	FinalityConsensus: "Finality Consensus", // TODO: use a different name when we have a better one
}

// MaxEpoch represents undefined epoch for not-yet-scheduled forks
var MaxEpoch = phase0.Epoch(math.MaxUint64)

// SSVFork describes a single SSV protocol fork.
type SSVFork struct {
	// Name of the fork
	Name string

	// Epoch when the fork is activated
	Epoch phase0.Epoch
}

// SSVForks is a list of SSV protocol forks.
type SSVForks []*SSVFork

// ActiveFork returns the active fork at the given epoch.
func (f SSVForks) ActiveFork(epoch phase0.Epoch) *SSVFork {
	for i := len(f) - 1; i >= 0; i-- {
		if f[i].Epoch <= epoch {
			return f[i]
		}
	}
	return nil
}

// ComputeActiveFork returns the active fork for the given epoch
func (f SSVForks) ComputeActiveFork(epoch phase0.Epoch) *SSVFork {
	// Search from the end to find the most recent active fork
	for i := len(f) - 1; i >= 0; i-- {
		if epoch >= f[i].Epoch {
			return f[i]
		}
	}
	// If no active fork is found, return nil
	return nil
}

// FindByName returns the fork with the given name, or nil if not found.
func (f SSVForks) FindByName(name string) *SSVFork {
	for _, fork := range f {
		if fork.Name == name {
			return fork
		}
	}
	return nil
}

// IsForkActive returns whether the fork with the given name is active at the given epoch.
func (f SSVForks) IsForkActive(name string, epoch phase0.Epoch) bool {
	fork := f.FindByName(name)
	if fork == nil {
		return false
	}

	activeFork := f.ActiveFork(epoch)
	return activeFork != nil && activeFork.Name == name
}

// SSVForkConfig contains fork configurations for an SSV network.
type SSVForkConfig struct {
	// Forks is the list of all SSV protocol forks in order of activation epoch.
	Forks SSVForks
}

// ActiveFork returns the active fork at the given epoch.
func (c SSVForkConfig) ActiveFork(epoch phase0.Epoch) *SSVFork {
	return c.Forks.ActiveFork(epoch)
}

// FindForkByName returns the fork with the given name, or nil if not found.
func (c SSVForkConfig) FindForkByName(name string) *SSVFork {
	return c.Forks.FindByName(name)
}

// IsForkActive returns whether the fork with the given name is active at the given epoch.
func (c SSVForkConfig) IsForkActive(name string, epoch phase0.Epoch) bool {
	return c.Forks.IsForkActive(name, epoch)
}

// GetGasLimit36Epoch returns the epoch at which the Gas Limit 36M fork is activated.
// This fork upgrades the default gas limit value from 30_000_000 to 36_000_000.
// If the fork is not found, returns MaxEpoch (undefined).
func (c SSVForkConfig) GetGasLimit36Epoch() phase0.Epoch {
	fork := c.FindForkByName("Gas Limit 36M")
	if fork != nil {
		return fork.Epoch
	}
	return MaxEpoch
}

// GetFinalityConsensusEpoch returns the epoch at which the Finality Consensus fork is activated.
// If the fork is not found, returns MaxEpoch (undefined).
func (c SSVForkConfig) GetFinalityConsensusEpoch() phase0.Epoch {
	fork := c.FindForkByName("Finality Consensus")
	if fork != nil {
		return fork.Epoch
	}
	return MaxEpoch
}
