package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStart_SilentLeaderSkipsProposalBroadcast(t *testing.T) {
	net := &recordingNetwork{}
	env := newInstanceTestEnv(t, 1)
	env.config.QBFTSilentLeader = true
	env.setNetwork(net)

	env.inst.Start(t.Context(), []byte("start-value"), specqbft.FirstHeight, testValueChecker{})

	require.Empty(t, net.broadcasted)
}

func TestStart_SilentLeaderFalseStillBroadcastsProposal(t *testing.T) {
	net := &recordingNetwork{}
	env := newInstanceTestEnv(t, 1)
	env.config.QBFTSilentLeader = false
	env.setNetwork(net)

	env.inst.Start(t.Context(), []byte("start-value"), specqbft.FirstHeight, testValueChecker{})

	require.Len(t, net.broadcasted, 1)
	proc, err := specqbft.NewProcessingMessage(net.broadcasted[0])
	require.NoError(t, err)
	require.Equal(t, specqbft.ProposalMsgType, proc.QBFTMessage.MsgType)
}

func TestUponRoundChange_SilentLeaderSkipsProposalBroadcast(t *testing.T) {
	net := &recordingNetwork{}
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.config.QBFTSilentLeader = true
	env.setNetwork(net)
	env.inst.State.Round = 2
	env.inst.StartValue = []byte("start-value")

	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	)

	err := env.inst.uponRoundChange(
		context.Background(),
		zap.NewNop(),
		env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
	)
	require.NoError(t, err)

	require.Empty(t, net.broadcasted)
}
