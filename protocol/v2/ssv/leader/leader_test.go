package leader_test

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/leader"
)

func expectedIndex(height specqbft.Height, round specqbft.Round, committeeSize uint64, offset uint64) uint64 {
	if committeeSize == 0 {
		return 0
	}

	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % committeeSize
	}

	return (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound) + offset) % committeeSize
}

func testNetCfg(slotsPerEpoch uint64, booleForkEpoch phase0.Epoch) *networkconfig.Network {
	return &networkconfig.Network{
		Beacon: &networkconfig.Beacon{
			SlotsPerEpoch: slotsPerEpoch,
		},
		SSV: &networkconfig.SSV{
			Forks: networkconfig.SSVForks{
				Boole: booleForkEpoch,
			},
		},
	}
}

func testState(height specqbft.Height, committee []spectypes.OperatorID) *specqbft.State {
	members := make([]*spectypes.Operator, 0, len(committee))
	for _, id := range committee {
		members = append(members, &spectypes.Operator{OperatorID: id})
	}

	return &specqbft.State{
		Height: height,
		CommitteeMember: &spectypes.CommitteeMember{
			Committee: members,
		},
	}
}

func TestLeaderFor_PreFork_NoEpochOffset(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	height := specqbft.Height(20)
	round := specqbft.FirstRound

	netCfg := testNetCfg(10, phase0.Epoch(100)) // epoch(height)=2, still pre-fork

	got := leader.For(height, round, committee, netCfg)
	want := committee[expectedIndex(height, round, uint64(len(committee)), 0)]
	require.Equal(t, want, got)

	gotState := leader.ForState(testState(height, committee), round, netCfg)
	require.Equal(t, want, gotState)
}

func TestLeaderFor_BooleFork_UsesEpochOffsetFromCfg(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	height := specqbft.Height(20)
	round := specqbft.FirstRound

	// SlotsPerEpoch is intentionally not 32 to ensure the offset is derived from config.
	netCfg := testNetCfg(10, phase0.Epoch(0))
	ethEpoch := uint64(netCfg.EstimatedEpochAtSlot(phase0.Slot(height))) // should be 2 when slotsPerEpoch=10 and height=20

	got := leader.For(height, round, committee, netCfg)
	want := committee[expectedIndex(height, round, uint64(len(committee)), ethEpoch)]
	require.Equal(t, want, got)

	gotState := leader.ForState(testState(height, committee), round, netCfg)
	require.Equal(t, want, gotState)
}

func TestLeaderFor_EmptyCommittee(t *testing.T) {
	netCfg := testNetCfg(32, phase0.Epoch(0))
	require.Equal(t, spectypes.OperatorID(0), leader.For(1, specqbft.FirstRound, nil, netCfg))
	require.Equal(t, spectypes.OperatorID(0), leader.ForState(testState(1, nil), specqbft.FirstRound, netCfg))
}
