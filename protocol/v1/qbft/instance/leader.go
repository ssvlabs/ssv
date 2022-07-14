package instance

import specqbft "github.com/bloxapp/ssv-spec/qbft"

// IsLeader checks and return true for round leader, false otherwise
func (i *Instance) IsLeader() bool {
	return uint64(i.ValidatorShare.NodeID) == i.ThisRoundLeader()
}

// ThisRoundLeader returns the round leader
func (i *Instance) ThisRoundLeader() uint64 {
	return i.RoundLeader(i.State().GetRound())
}

// RoundLeader checks the round leader
func (i *Instance) RoundLeader(round specqbft.Round) uint64 {
	leaderIndex := i.LeaderSelector.Calculate(uint64(round))
	return i.ValidatorShare.OperatorIds[leaderIndex]
}
