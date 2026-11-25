package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Pins the bump-before-propose behavior from 5ff4c098: a leader that learns of a
// round-change quorum for a future round it leads must propose for that round.
// A stray round change for a lower round makes the partial-quorum path move the leader
// only to that lower round, so bumpToRound runs before CreateProposal: the proposal must
// carry the round its signer leads and that round's round changes. Found by FuzzInstance.
func TestUponRoundChangeFutureRoundQuorumProposesJustifiedRound(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.config.ProposerF = roundRobin // 1 leads rounds 1, 5, 9, ...
	env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})
	require.Equal(t, specqbft.Round(1), env.inst.State.Round)
	broadcastsBefore := len(env.network.BroadcastedMsgs)

	process := func(pm *specqbft.ProcessingMessage) {
		_, _, _, err := env.inst.ProcessMsg(context.Background(), zap.NewNop(), pm)
		require.NoError(t, err)
	}
	unpreparedRoundChange := func(round specqbft.Round, signer spectypes.OperatorID) *specqbft.ProcessingMessage {
		return env.roundChange(round, signer, specqbft.NoRound, [32]byte{}, nil, nil)
	}

	// One operator sends a round change for round 3 (it is unprepared and well-formed).
	// A single future-round signer is below f+1, so the leader stays in round 1.
	process(unpreparedRoundChange(3, 2))
	process(unpreparedRoundChange(5, 2))
	require.Equal(t, specqbft.Round(1), env.inst.State.Round)
	// A second signer makes a partial quorum. Its lowest round is 3, so the leader moves there.
	process(unpreparedRoundChange(5, 3))
	require.Equal(t, specqbft.Round(3), env.inst.State.Round,
		"precondition: the leader must be below round 5 when the round-5 quorum completes")
	// The third signer completes the quorum for round 5, which operator 1 leads.
	process(unpreparedRoundChange(5, 4))
	require.Equal(t, specqbft.Round(5), env.inst.State.Round, "leader must end in the round it proposed for")

	var proposals []*specqbft.ProcessingMessage
	for _, sm := range env.network.BroadcastedMsgs[broadcastsBefore:] {
		pm, err := specqbft.NewProcessingMessage(sm)
		require.NoError(t, err)
		if pm.QBFTMessage.MsgType == specqbft.ProposalMsgType {
			proposals = append(proposals, pm)
		}
	}
	require.Len(t, proposals, 1, "leader of round 5 must propose once it has a round-change quorum for round 5")
	proposal := proposals[0]
	require.Equal(t, specqbft.Round(5), proposal.QBFTMessage.Round, "proposal must be for the justified round")

	// A fresh node in round 5 must accept it.
	peer := newInstanceTestEnv(t, 2)
	peer.config.ProposerF = roundRobin
	peer.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})
	peer.inst.State.Round = 5
	require.NoError(t, peer.inst.isValidProposal(proposal))
}

// roundRobin rotates the leader through the 4-operator test committee by round only.
func roundRobin(_ *specqbft.State, round specqbft.Round) spectypes.OperatorID {
	return spectypes.OperatorID((round-1)%4) + 1
}
