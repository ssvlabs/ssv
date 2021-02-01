package ibft

func (i *Instance) IsLeader() bool {
	return i.Me.IbftId == i.ThisRoundLeader()
}

func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.State.Round)
}

func (i *Instance) RoundLeader(round uint64) uint64 {
	return round % uint64(i.params.CommitteeSize())
}
