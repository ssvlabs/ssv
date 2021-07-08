package ibft

// IsLeader checks and return true for round leader, false otherwise
func (i *Instance) IsLeader() bool {
	return i.ValidatorShare.NodeID == i.ThisRoundLeader()
}

// ThisRoundLeader returns the round leader
func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.State.Round) + 1 // node ids start from 1
}

// RoundLeader checks the round leader
func (i *Instance) RoundLeader(round uint64) uint64 {
	return i.LeaderSelector.Calculate(round)
}
