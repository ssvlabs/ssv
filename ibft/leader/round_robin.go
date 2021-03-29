package leader

// Round robin leader selection is a fair and sequential leader selection.
// Each instance/ round change the next leader is selected one-by-one.
type RoundRobin struct {
	index uint64
}

func (rr *RoundRobin) Current(committeeSize uint64) uint64 {
	return rr.index % committeeSize
}

func (rr *RoundRobin) Bump() {
	rr.index++
}
