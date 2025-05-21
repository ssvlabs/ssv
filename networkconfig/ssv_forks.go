package networkconfig

import "github.com/attestantio/go-eth2-client/spec/phase0"

// Fork is a numerical identifier of specific network upgrades (forks).
type Fork int

const (
	Alan              Fork = iota
	FinalityConsensus      // TODO: use a different name when we have a better one
)

// String implements fmt.Stringer.
func (f Fork) String() string {
	s, ok := forkToString[f]
	if !ok {
		return "Unknown fork"
	}
	return s
}

var forkToString = map[Fork]string{
	Alan:              "Alan",
	FinalityConsensus: "Finality Consensus", // TODO: use a different name when we have a better one
}

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

// IsFinalityConsensusActive returns whether the FinalityConsensus fork is active at the given epoch.
// TODO: use a different name when we have a better one
func (c SSVForkConfig) IsFinalityConsensusActive(epoch phase0.Epoch) bool {
	return c.IsForkActive("Finality Consensus", epoch)
}
