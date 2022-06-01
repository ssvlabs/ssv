package leader

// Selector is interface to implement the leader selection logic
type Selector interface {
	// Calculate returns the current leader as calculated by the implementation.
	Calculate(round uint64) uint64
}
