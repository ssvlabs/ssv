package commit

import (
	"bytes"
	"errors"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ErrInvalidSignersNum represents an error when the number of signers is invalid.
var ErrInvalidSignersNum = errors.New("commit msgs allow 1 signer")

// ValidateCommitData validates message commit data.
func ValidateCommitData() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate commit data", func(signedMessage *specqbft.SignedMessage) error {
		msgCommitData, err := signedMessage.Message.GetCommitData()
		if err != nil {
			return fmt.Errorf("could not get msg commit data: %w", err)
		}

		if err := msgCommitData.Validate(); err != nil {
			return fmt.Errorf("msgCommitData invalid: %w", err)
		}

		return nil
	})
}

// ValidateMsgSigners validates commit message signers.
func ValidateMsgSigners() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate signers", func(signedMessage *specqbft.SignedMessage) error {
		signers := signedMessage.GetSigners()
		if len(signers) != 1 {
			return ErrInvalidSignersNum
		}

		return nil
	})
}

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

		if !bytes.Equal(proposedData.Data, msgCommitData.Data) {
			return fmt.Errorf("message data is different from proposed data")
		}

		return nil
	})
}

// ValidateDecidedQuorum validates decided message quorum.
func ValidateDecidedQuorum(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate decided quorum", func(signedMessage *specqbft.SignedMessage) error {
		if quorum, _, _ := signedmsg.HasQuorum(share, []*specqbft.SignedMessage{signedMessage}); !quorum {
			return fmt.Errorf("not a decided msg")
		}

		return nil
	})
}
