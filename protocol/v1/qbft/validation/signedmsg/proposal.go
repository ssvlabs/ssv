package signedmsg

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ProposalExists checks if proposal for current was received.
func ProposalExists(state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate proposal", func(signedMessage *specqbft.SignedMessage) error {
		proposedMsg := state.GetProposalAcceptedForCurrentRound()
		if proposedMsg == nil {
			return fmt.Errorf("did not receive proposal for this round")
		}

		return nil
	})
}
