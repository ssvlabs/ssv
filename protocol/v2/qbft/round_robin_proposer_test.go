package qbft

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// preBooleForkNetConfig is a stub networkConfig whose fork gate is always off, exercising
// Proposer's pre-fork branch.
type preBooleForkNetConfig struct{}

func (preBooleForkNetConfig) EstimatedEpochAtSlot(phase0.Slot) phase0.Epoch { return 0 }
func (preBooleForkNetConfig) BooleForkAtSlot(phase0.Slot) bool              { return false }

func TestRoundRobinProposerPreBooleFork_MatchesStageLogic(t *testing.T) {
	height := specqbft.Height(20)
	committee := []spectypes.OperatorID{1, 2, 3, 4}

	// height%4=0 so leader should be the first operator for FirstRound.
	require.Equal(t, spectypes.OperatorID(1), Proposer(height, specqbft.FirstRound, committee, preBooleForkNetConfig{}))
}

func TestRoundRobinProposer_PostBooleFork_OffsetFromSlotsPerEpoch(t *testing.T) {
	netCfg := &networkconfig.Network{
		Beacon: &networkconfig.Beacon{SlotsPerEpoch: 10},
	}
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	height := specqbft.Height(20) // epoch=2 (derived from slotsPerEpoch=10), so offset=2

	// height%4=0 and offset=2 -> index=2 -> third operator.
	require.Equal(t, spectypes.OperatorID(3), RoundRobinProposer(height, specqbft.FirstRound, committee, netCfg))
}

func TestRoundRobinProposer_CommitteeOrderDoesNotMatter(t *testing.T) {
	netCfg := &networkconfig.Network{
		Beacon: &networkconfig.Beacon{SlotsPerEpoch: 32},
	}
	height := specqbft.Height(2138369) // epoch=66824 (%4==0), height%4==1 -> index=1 -> operator 10
	round := specqbft.FirstRound

	require.Equal(t, spectypes.OperatorID(10), RoundRobinProposer(height, round, []spectypes.OperatorID{9, 10, 11, 12}, netCfg))
	require.Equal(t, spectypes.OperatorID(10), RoundRobinProposer(height, round, []spectypes.OperatorID{12, 9, 10, 11}, netCfg))
}
