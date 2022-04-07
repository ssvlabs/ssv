package constant

import "github.com/bloxapp/ssv/protocol/v1/message"

// Constant robin leader selection will always return the same leader
type Constant struct {
	LeaderIndex uint64
}

// Calculate returns the current leader
func (rr *Constant) Calculate(round message.Round) uint64 {
	return rr.LeaderIndex
}
