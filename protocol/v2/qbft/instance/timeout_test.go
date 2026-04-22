package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUponRoundTimeoutBumpsRoundAfterBroadcast(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.State.Round = 1
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, []byte("proposal-value"), env.hash([]byte("proposal-value")), nil, nil)
	env.inst.State.LastPreparedRound = 1
	env.inst.State.LastPreparedValue = []byte("prepared-value")

	root := env.hash(env.inst.State.LastPreparedValue)
	env.addMessages(
		env.inst.State.PrepareContainer,
		env.prepare(1, 1, root),
		env.prepare(1, 2, root),
		env.prepare(1, 3, root),
	)

	network := &recordingNetwork{
		onBroadcast: func(message *spectypes.SignedSSVMessage) error {
			require.Equal(t, specqbft.Round(1), env.inst.State.Round)
			require.NotNil(t, env.inst.State.ProposalAcceptedForCurrentRound)
			return nil
		},
	}
	env.setNetwork(network)

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.NoError(t, err)

	require.Equal(t, specqbft.Round(2), env.inst.State.Round)
	require.Nil(t, env.inst.State.ProposalAcceptedForCurrentRound)
	require.Equal(t, 1, env.roundTimer.State.Timeouts)
	require.Equal(t, specqbft.Round(2), env.roundTimer.State.Round)
	require.Len(t, network.broadcasted, 1)

	msg, err := specqbft.NewProcessingMessage(network.broadcasted[0])
	require.NoError(t, err)
	require.Equal(t, specqbft.RoundChangeMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	require.Equal(t, specqbft.Round(1), msg.QBFTMessage.DataRound)
	require.Equal(t, root, msg.QBFTMessage.Root)
	require.Equal(t, env.inst.State.LastPreparedValue, msg.SignedMessage.FullData)
}

func TestUponRoundTimeoutKilledInstance(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.Kill()

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance stopped processing timeouts")
}

func TestUponRoundTimeoutStopsProcessingAfterReachingCutOffRound(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.StartValue = []byte("start-value")
	env.config.CutOffRound = env.inst.State.Round + 1

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, specqbft.Round(2), env.inst.State.Round)

	err = env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance stopped processing timeouts")
}
