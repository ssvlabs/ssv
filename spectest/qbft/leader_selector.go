package qbft

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
)

// nolint:unused
type roundRobinLeaderSelector struct {
	state       *qbftprotocol.State
	mappedShare *spectypes.Share
}

// nolint:unused
func (m roundRobinLeaderSelector) Calculate(round uint64) uint64 {
	specState := &specqbft.State{
		Share:  m.mappedShare,
		Height: m.state.GetHeight(),
	}
	// RoundRobinProposer returns OperatorID which starts with 1.
	// As the result will be used to index OperatorIds which starts from 0,
	// the result of Calculate should be decremented.
	return uint64(specqbft.RoundRobinProposer(specState, specqbft.Round(round))) - 1
}
