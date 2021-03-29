package leader

type Selector interface {
	// Current returns the current leader as calculated by the implementation.
	Current(committeeSize uint64) uint64

	// Bump is a util function used to keep track of round and instance changes internally to the implementation.
	Bump()
}
