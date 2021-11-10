package v1

import "github.com/bloxapp/ssv/network/forks"

// HighestDecidedStreamProtocol implementation
func (v1 *ForkV1) HighestDecidedStreamProtocol() string {
	if v1.forked() {
		return forks.HighestDecidedStream
	}
	return v1.forkV0.HighestDecidedStreamProtocol()
}

// DecidedByRangeStreamProtocol implementation
func (v1 *ForkV1) DecidedByRangeStreamProtocol() string {
	if v1.forked() {
		return forks.DecidedByRangeStream
	}
	return v1.forkV0.DecidedByRangeStreamProtocol()
}

// LastChangeRoundStreamProtocol implementation
func (v1 *ForkV1) LastChangeRoundStreamProtocol() string {
	if v1.forked() {
		return forks.LastChangeRoundMsgStream
	}
	return v1.forkV0.LastChangeRoundStreamProtocol()
}
