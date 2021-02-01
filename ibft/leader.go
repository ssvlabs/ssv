package ibft

func (i *Instance) IsLeader() bool {
	return i.me.IbftId == i.ThisRoundLeader()
}

func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.state.Round)
}

func (i *Instance) RoundLeader(round uint64) uint64 {
	return round % uint64(i.params.CommitteeSize())
}
