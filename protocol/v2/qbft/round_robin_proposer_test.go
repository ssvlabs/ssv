package qbft

import (
	"testing"

	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

func TestRoundRobinProposer_PreFork_NoOffset(t *testing.T) {
	netCfg := &networkconfig.Network{
		Beacon: &networkconfig.Beacon{SlotsPerEpoch: 10},
		SSV:    &networkconfig.SSV{Forks: networkconfig.SSVForks{Boole: 100}},
	}
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	height := specqbft.Height(20) // epoch=2, but still pre-fork
	round := specqbft.FirstRound

	// height%4=0 so leader should be the first operator when offset=0.
	require.Equal(t, spectypes.OperatorID(1), RoundRobinProposer(height, round, committee, netCfg))
}

func TestRoundRobinProposer_BooleFork_OffsetFromSlotsPerEpoch(t *testing.T) {
	netCfg := &networkconfig.Network{
		Beacon: &networkconfig.Beacon{SlotsPerEpoch: 10},
		SSV:    &networkconfig.SSV{Forks: networkconfig.SSVForks{Boole: 0}},
	}
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	height := specqbft.Height(20) // epoch=2 (derived from slotsPerEpoch=10), so offset=2
	round := specqbft.FirstRound

	// height%4=0 and offset=2 -> index=2 -> third operator.
	require.Equal(t, spectypes.OperatorID(3), RoundRobinProposer(height, round, committee, netCfg))
}
