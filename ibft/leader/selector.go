package leader

// Selector is interface to implement the leader selection logic
type Selector interface {
	// Current returns the current leader as calculated by the implementation.
	Current(committeeSize uint64) uint64

	// Bump is a util function used to keep track of round and instance changes internally to the implementation.
	Bump()

	// SetSeed sets seed from which the leader is deterministically determined
	SetSeed(seed []byte, index uint64) error
}
