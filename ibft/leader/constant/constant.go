package leader

// Constant robin leader selection will always return the same leader
type Constant struct {
	LeaderIndex uint64
}

// Current returns the current leader
func (rr *Constant) Current(committeeSize uint64) uint64 {
	return rr.LeaderIndex
}

// Bump to the index
func (rr *Constant) Bump() {
}

// SetSeed takes []byte and converts to uint64,returns error if fails.
func (rr *Constant) SetSeed(seed []byte, index uint64) error {
	return nil
}
