package leader

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/networkconfig"
	qbft "github.com/ssvlabs/ssv/protocol/v2/qbft"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func proposerIndex(height specqbft.Height, round specqbft.Round, committeeSize uint64, netCfg *networkconfig.Network) uint64 {
	if committeeSize == 0 {
		return 0
	}

	slot := phase0.Slot(height)
	if netCfg.BooleForkAtSlot(slot) {
		ethEpoch := uint64(netCfg.EstimatedEpochAtSlot(slot))
		return qbft.RoundRobinIndex(height, round, committeeSize, ethEpoch)
	}

	return qbft.RoundRobinIndex(height, round, committeeSize, 0)
}

// For returns the leader for a given height/round and committee, taking forks into account based on the duty slot.
func For(height specqbft.Height, round specqbft.Round, committee []spectypes.OperatorID, netCfg *networkconfig.Network) spectypes.OperatorID {
	if len(committee) == 0 {
		return 0
	}
	index := proposerIndex(height, round, uint64(len(committee)), netCfg)
	return committee[index]
}

// ForState is a convenience wrapper around For for QBFT state.
func ForState(state *specqbft.State, round specqbft.Round, netCfg *networkconfig.Network) spectypes.OperatorID {
	if len(state.CommitteeMember.Committee) == 0 {
		return 0
	}
	index := proposerIndex(state.Height, round, uint64(len(state.CommitteeMember.Committee)), netCfg)
	return state.CommitteeMember.Committee[index].OperatorID
}
