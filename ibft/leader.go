package ibft

// IsLeader checks and return true for round leader, false otherwise
func (i *Instance) IsLeader() bool {
	return i.Me.IbftId == i.ThisRoundLeader()
}

// ThisRoundLeader returns the round leader
func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.State.Round)
}

// RoundLeader checks the round leader
func (i *Instance) RoundLeader(round uint64) uint64 {
	return i.LeaderSelector.Current(uint64(i.Params.CommitteeSize()))
}
