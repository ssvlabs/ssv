package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUponRoundTimeoutBumpsRound(t *testing.T) {
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
			require.Equal(t, specqbft.Round(2), env.inst.State.Round)
			require.Nil(t, env.inst.State.ProposalAcceptedForCurrentRound)
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
	env.inst.MarkIrrelevant()

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance is no longer considered relevant")
}

func TestUponRoundTimeoutStopsProcessingAfterReachingCutOffRound(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.StartValue = []byte("start-value")
	// CutOffRound == State.Round+1, so the first timeout bumps the instance *into* the cutoff round — its
	// give-up point. It stops there quietly: no timer armed, no round-change broadcast, and it returns nil
	// rather than an error (which would redden spans on every failed duty). The second call then hits the
	// plain already-at-cutoff path, which still reports the instance as no longer relevant.
	env.config.CutOffRound = env.inst.State.Round + 1

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, env.config.CutOffRound, env.inst.State.Round)
	require.Zero(t, env.roundTimer.State.Timeouts, "no timer armed once the instance gives up at the cutoff")
	require.Empty(t, env.network.BroadcastedMsgs, "no round-change broadcast at the cutoff")

	err = env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance is no longer considered relevant")
	require.Equal(t, env.config.CutOffRound, env.inst.State.Round)
}
