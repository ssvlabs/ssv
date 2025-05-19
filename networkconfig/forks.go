package networkconfig

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

// ForksConfig contains the epoch numbers for different protocol forks.
type ForksConfig struct {
	// FinalityConsensusEpoch is the epoch at which the network switches
	// to using finalized blocks for determining log fetching ranges and health checks.
	FinalityConsensusEpoch uint64 // TODO: use a different name when we have a better one
}
