package proposal

import "github.com/bloxapp/ssv/spec/qbft/spectest/tests"

// PreviousProposalForRound tests a second proposal (by same signer) for current round. state.ProposalAcceptedForCurrentRound != nil && signedProposal.Message.Round == state.Round
func PreviousProposalForRound() *tests.MsgProcessingSpecTest {
	panic("implement")
}
