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

// A non-leader keeps its start value for a later round it may lead and broadcasts nothing at start.
func TestInstance_StartAsNonLeaderDoesNotPropose(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(2)

	value := []byte("start-value")
	env.inst.Start(context.Background(), value, testValueChecker{})

	require.Equal(t, value, env.inst.StartValue)
	require.Empty(t, env.network.BroadcastedMsgs)
}

// A leader that comes to lead a round with a prepared value re-proposes that value over its own start
// value: once a proposal was prepared, only it may progress.
func TestUponRoundChangeLeaderReproposesPreparedValueOverOwn(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.inst.State.Round = 2
	env.inst.StartValue = []byte("own-start-value")

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
