package proposal

import "github.com/bloxapp/ssv/spec/qbft/spectest/tests"
"

// FutureRound tests a proposal for state.ProposalAcceptedForCurrentRound == nil && signedProposal.Message.Round > state.Round
func FutureRound() *tests.MsgProcessingSpecTest {
	panic("implement")
}
