package constant

// Constant robin leader selection will always return the same leader
type Constant struct {
	LeaderIndex uint64
}

// Calculate returns the current leader
func (rr *Constant) Calculate(round uint64) uint64 {
	return rr.LeaderIndex
}
