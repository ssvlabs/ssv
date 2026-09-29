package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/qa/faults"
)

// roundThreeEnv builds an instance at round 2 whose next timeout bumps it into the cutoff round 3 —
// the proposer's shape since ssv#3041 (roundtimer.MaxRound(RoleProposer) = 2, cutoff 3).
func roundThreeEnv(t *testing.T, role spectypes.RunnerRole) *instanceTestEnv {
	t.Helper()
	env := newInstanceTestEnv(t, 2)
	id := spectypes.NewValidatorMsgID(spectestingutils.TestingSSVDomainType, spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey), role)
	env.inst.State.ID = id[:]
	env.inst.State.Height = specqbft.Height(1234)
	env.inst.State.Round = 2
	env.config.CutOffRound = 3
	return env
}

func observedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	return zap.New(core), logs
}

// TestProposerRound3Fault pins QA fault proposer-round-3 (TIM-08 ignore leg): on the proposer's
// round-2 expiry the instance gives up at the cutoff as usual, but first broadcasts ONE round-change
// for round 3, signed with this operator's key, straight to the network (Instance.Broadcast refuses
// an instance that is no longer relevant).
func TestProposerRound3Fault(t *testing.T) {
	t.Run("proposer, fault on: one RoundChange(3), then the instance still stops", func(t *testing.T) {
		faults.SetForTest(t, faults.ProposerRound3)
		env := roundThreeEnv(t, spectypes.RoleProposer)
		logger, logs := observedLogger()

		require.NoError(t, env.inst.UponRoundTimeout(t.Context(), logger))

		require.Len(t, env.network.BroadcastedMsgs, 1)
		msg, err := specqbft.NewProcessingMessage(env.network.BroadcastedMsgs[0])
		require.NoError(t, err)
		require.Equal(t, specqbft.RoundChangeMsgType, msg.QBFTMessage.MsgType)
		require.Equal(t, specqbft.Round(3), msg.QBFTMessage.Round)
		require.Equal(t, specqbft.Height(1234), msg.QBFTMessage.Height)
		require.Equal(t, env.inst.State.ID, msg.QBFTMessage.Identifier)
		require.Equal(t, []spectypes.OperatorID{2}, msg.SignedMessage.OperatorIDs, "signed with this operator's key")

		require.Equal(t, specqbft.Round(3), env.inst.State.Round)
		require.False(t, env.inst.IsRelevant(), "the instance still gives up at the cutoff")
		require.Zero(t, env.roundTimer.State.Timeouts, "no timer armed")

		require.Equal(t, 1, logs.FilterMessage("🧪 qa fault injected").Len())
		fired := logs.FilterMessage("🧪 qa fault injected").All()[0].ContextMap()
		require.Equal(t, string(faults.ProposerRound3), fired["qa_fault"])
		require.EqualValues(t, 3, fired["qbft_round"])
		require.EqualValues(t, 1234, fired["slot"])
		require.Equal(t, "PROPOSER", fired["role"])
		require.Equal(t, 1, logs.FilterMessage("📢 broadcasting round change message (this round timed out)").
			FilterField(zap.Uint64("qbft_new_round", 3)).Len(), "the card's op5 grep string")
		require.Equal(t, 1, logs.FilterMessage("instance reached its cutoff round, giving up").Len())

		// Fires once per instance: a later timeout hits the not-relevant guard and sends nothing.
		require.ErrorContains(t, env.inst.UponRoundTimeout(t.Context(), logger), "no longer considered relevant")
		require.Len(t, env.network.BroadcastedMsgs, 1)
		require.Equal(t, 1, logs.FilterMessage("🧪 qa fault injected").Len())
	})

	t.Run("proposer, fault off: stock give-up, nothing broadcast", func(t *testing.T) {
		env := roundThreeEnv(t, spectypes.RoleProposer)
		logger, logs := observedLogger()

		require.NoError(t, env.inst.UponRoundTimeout(t.Context(), logger))
		require.Empty(t, env.network.BroadcastedMsgs)
		require.Zero(t, logs.FilterMessage("🧪 qa fault injected").Len())
	})

	t.Run("committee, fault on: nothing extra", func(t *testing.T) {
		faults.SetForTest(t, faults.ProposerRound3)
		env := roundThreeEnv(t, spectypes.RoleCommittee)
		logger, logs := observedLogger()

		require.NoError(t, env.inst.UponRoundTimeout(t.Context(), logger))
		require.Empty(t, env.network.BroadcastedMsgs)
		require.Zero(t, logs.FilterMessage("🧪 qa fault injected").Len())
	})

	t.Run("proposer, fault on, cutoff reached at a round other than 3: nothing extra", func(t *testing.T) {
		faults.SetForTest(t, faults.ProposerRound3)
		env := roundThreeEnv(t, spectypes.RoleProposer)
		env.inst.State.Round = 1
		env.config.CutOffRound = 2

		require.NoError(t, env.inst.UponRoundTimeout(t.Context(), zap.NewNop()))
		require.Empty(t, env.network.BroadcastedMsgs)
	})

	t.Run("proposer, fault on, round 1 expiry: only the stock RoundChange(2)", func(t *testing.T) {
		faults.SetForTest(t, faults.ProposerRound3)
		env := roundThreeEnv(t, spectypes.RoleProposer)
		env.inst.State.Round = 1

		require.NoError(t, env.inst.UponRoundTimeout(t.Context(), zap.NewNop()))
		require.Len(t, env.network.BroadcastedMsgs, 1)
		msg, err := specqbft.NewProcessingMessage(env.network.BroadcastedMsgs[0])
		require.NoError(t, err)
		require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	})
}
