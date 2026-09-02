package instance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// A leader that starts with a value proposes it (the regular StartNewInstance path).
func TestInstance_StartWithValueProposes(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)

	value := []byte("start-value")
	env.inst.Start(context.Background(), value, testValueChecker{})

	require.Equal(t, value, env.inst.StartValue)
	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.ProposalMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.FirstRound, msg.QBFTMessage.Round)
	require.Equal(t, env.hash(value), msg.QBFTMessage.Root)
}

// A leader that joined without a value (a voter, see controller.JoinInstance) must not broadcast an
// empty proposal; it leaves round 1 to time out.
func TestInstance_StartWithoutValueDoesNotPropose(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)

	env.inst.Start(context.Background(), nil, testValueChecker{})

	require.Empty(t, env.inst.StartValue)
	require.Empty(t, env.network.BroadcastedMsgs)
}

// A non-leader that joined without a value starts like any other non-leader: nothing to broadcast.
func TestInstance_StartWithoutValueAsNonLeader(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(2)

	env.inst.Start(context.Background(), nil, testValueChecker{})

	require.Empty(t, env.network.BroadcastedMsgs)
}

// A voter that comes to lead a round with no prepared value to re-propose must not broadcast an empty
// proposal: the empty start value fails the proposal justification's value check, so the round is left
// to time out.
func TestUponRoundChangeAsVoterLeaderWithoutPreparedValueSkipsProposal(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.inst.State.Round = 2
	env.inst.StartValue = nil

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

	require.Empty(t, env.network.BroadcastedMsgs)
	require.Equal(t, specqbft.Round(2), env.inst.State.Round)
}

// A voter that comes to lead a round with a prepared value re-proposes that value: it needs no value of
// its own for the round to progress once the builder's proposal was prepared.
func TestUponRoundChangeAsVoterLeaderReproposesPreparedValue(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.inst.State.Round = 2
	env.inst.StartValue = nil

	prepared := []byte("prepared-value")
	root := env.hash(prepared)
	prepares := []*specqbft.ProcessingMessage{
		env.prepare(1, 1, root),
		env.prepare(1, 2, root),
		env.prepare(1, 3, root),
	}
	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 2, 1, root, prepared, prepares),
		env.roundChange(2, 3, 1, root, prepared, prepares),
	)

	err := env.inst.uponRoundChange(
		context.Background(),
		zap.NewNop(),
		env.roundChange(2, 4, 1, root, prepared, prepares),
	)
	require.NoError(t, err)

	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.ProposalMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	require.Equal(t, root, msg.QBFTMessage.Root)
	require.Equal(t, prepared, msg.SignedMessage.FullData)
}
