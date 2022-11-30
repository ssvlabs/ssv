package handlers

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

func extractSignedMessage(state *specqbft.State) (*specqbft.SignedMessage, error) {
	_, msgs := state.CommitContainer.LongestUniqueSignersForRoundAndValue(state.LastPreparedRound, state.DecidedValue)
	return instance.AggregateCommitMsgs(msgs)
}
