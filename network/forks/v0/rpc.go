package v0

import "github.com/bloxapp/ssv/network/forks"

// HighestDecidedStreamProtocol implementation
func (v0 *ForkV0) HighestDecidedStreamProtocol() string {
	return forks.LegacyMsgStream
}

// DecidedByRangeStreamProtocol implementation
func (v0 *ForkV0) DecidedByRangeStreamProtocol() string {
	return forks.LegacyMsgStream
}

// LastChangeRoundStreamProtocol implementation
func (v0 *ForkV0) LastChangeRoundStreamProtocol() string {
	return forks.LegacyMsgStream
}
