package instance

import "github.com/bloxapp/ssv/protocol/v1/message"

// IsLeader checks and return true for round leader, false otherwise
func (i *Instance) IsLeader() bool {
	return uint64(i.ValidatorShare.NodeID) == i.ThisRoundLeader()
}

// ThisRoundLeader returns the round leader
func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.State().GetRound())
}

// RoundLeader checks the round leader
func (i *Instance) RoundLeader(round message.Round) uint64 {
	return i.LeaderSelector.Calculate(round) + 1 // node ids start from 1
}
