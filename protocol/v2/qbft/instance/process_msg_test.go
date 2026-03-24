package instance

import (
	"context"
	"sync"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBaseMsgValidationPastRoundDecidedCommitBypassesPastRoundGuard(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	fullData := []byte("commit-value")
	root := env.hash(fullData)
	env.inst.State.Round = 2
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(2, 1, fullData, root, nil, nil)

	msg := env.aggregateMessages(
		env.commit(1, 1, root),
		env.commit(1, 2, root),
		env.commit(1, 3, root),
	)

	err := env.inst.BaseMsgValidation(msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "msg allows 1 signer")
}

func TestBaseMsgValidationPastRoundNonDecidedRejected(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	root := env.hash([]byte("proposal-value"))
	env.inst.State.Round = 2
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(2, 1, []byte("proposal-value"), root, nil, nil)

	err := env.inst.BaseMsgValidation(env.prepare(1, 1, root))
	require.ErrorContains(t, err, "past round")

	err = env.inst.BaseMsgValidation(env.commit(1, 1, root))
	require.ErrorContains(t, err, "past round")
}

func TestProcessMsgDispatchCommitUpdatesDecision(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	env.setLeader(1)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
	env.addMessages(
		env.inst.State.CommitContainer,
		env.commit(1, 1, root),
		env.commit(1, 2, root),
	)

	decided, decidedValue, aggregated, err := env.inst.ProcessMsg(
		context.Background(),
		zap.NewNop(),
		env.commit(1, 3, root),
	)
	require.NoError(t, err)
	require.True(t, decided)
	require.Equal(t, fullData, decidedValue)
	require.NotNil(t, aggregated)
	require.True(t, env.inst.State.Decided)
	require.Equal(t, fullData, env.inst.State.DecidedValue)
}

func TestProcessMsgConcurrentAccess(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
	env.addMessages(
		env.inst.State.PrepareContainer,
		env.prepare(1, 1, root),
		env.prepare(1, 3, root),
	)
	msg := env.prepare(1, 4, root)

	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Intentionally reuse the same message pointer to exercise dedup/idempotent
			// processing while ProcessMsg serializes handler execution through processMsgF.
			_, _, _, err := env.inst.ProcessMsg(context.Background(), zap.NewNop(), msg)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, specqbft.Round(1), env.inst.State.Round)
	require.Equal(t, fullData, env.inst.State.LastPreparedValue)
	require.Equal(t, specqbft.Round(1), env.inst.State.LastPreparedRound)
	require.Len(t, env.network.BroadcastedMsgs, 1)

	broadcasted, err := specqbft.NewProcessingMessage(env.network.BroadcastedMsgs[0])
	require.NoError(t, err)
	require.Equal(t, specqbft.CommitMsgType, broadcasted.QBFTMessage.MsgType)
}
