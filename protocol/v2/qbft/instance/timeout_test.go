package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
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
			// The round change is created and broadcast while the instance is still in the previous round —
			// the bump is deferred until after the broadcast, matching the ssv-spec UponRoundTimeout ordering.
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
	env.inst.MarkIrrelevant()

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance is no longer considered relevant")
}

func TestUponRoundTimeoutStopsProcessingAfterReachingCutOffRound(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.StartValue = []byte("start-value")
	// At the cut-off round the instance is no longer relevant: a timeout must be a no-op error and must neither
	// advance the round nor broadcast anything.
	env.inst.State.Round = env.config.CutOffRound

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance is no longer considered relevant")
	require.Equal(t, env.config.CutOffRound, env.inst.State.Round)

	err = env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.ErrorContains(t, err, "instance is no longer considered relevant")
	require.Equal(t, env.config.CutOffRound, env.inst.State.Round)

	require.Empty(t, env.network.BroadcastedMsgs)
}

// TestUponRoundTimeoutBroadcastsRoundChangeAtCutOffBoundary covers the round just below the cut-off round, where
// a timeout produces the terminal round-change. The instance is still relevant, so the round change must be
// broadcast and UponRoundTimeout must return nil before the round is bumped into the (now irrelevant) cut-off
// round. Bumping eagerly (the previous behavior) advanced the instance into the cut-off round mid-call, so the
// subsequent Broadcast rejected the message as no-longer-relevant — suppressing this final round-change and
// returning a spurious error, a deviation from the ssv-spec UponRoundTimeout.
func TestUponRoundTimeoutBroadcastsRoundChangeAtCutOffBoundary(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.StartValue = []byte("start-value")
	env.config.CutOffRound = roundtimer.CutOffRound
	env.inst.State.Round = roundtimer.CutOffRound - 1 // last relevant round

	err := env.inst.UponRoundTimeout(t.Context(), zap.NewNop())
	require.NoError(t, err)

	// The round was bumped into the cut-off round and the timer was scheduled for it.
	require.Equal(t, roundtimer.CutOffRound, env.inst.State.Round)
	require.Equal(t, roundtimer.CutOffRound, env.roundTimer.State.Round)
	require.Equal(t, 1, env.roundTimer.State.Timeouts)

	// The terminal round-change was broadcast, carrying the cut-off round.
	require.Len(t, env.network.BroadcastedMsgs, 1)
	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.RoundChangeMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, roundtimer.CutOffRound, msg.QBFTMessage.Round)
}
