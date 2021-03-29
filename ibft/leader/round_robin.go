package leader

type RoundRobin struct {
}

func (rr *RoundRobin) GetLeader() uint64 {
	return 0
}

func (rr *RoundRobin) BumpLeader() uint64 {
	return 0
}
