package commit

import (
	"bytes"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateProposal validates message against received proposal for this round.
func ValidateProposal(state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate proposal", func(signedMessage *specqbft.SignedMessage) error {
		proposedMsg := state.GetProposalAcceptedForCurrentRound()
		if proposedMsg == nil {
			return fmt.Errorf("did not receive proposal for this round")
		}

		proposedData, err := proposedMsg.Message.GetProposalData()
		if err != nil {
			return fmt.Errorf("could not get proposed data: %w", err)
		}

		msgCommitData, err := signedMessage.Message.GetCommitData()
		if err != nil {
			return fmt.Errorf("could not get msg commit data: %w", err)
		}

		if err := msgCommitData.Validate(); err != nil {
			return fmt.Errorf("msgCommitData invalid: %w", err)
		}

		if !bytes.Equal(proposedData.Data, msgCommitData.Data) {
			return fmt.Errorf("message data is different from proposed data")
		}

		return nil
	})
}
