package validation

// message_counts.go contains code for counting and validating messages per validator-slot-round.

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

// maxMessageCounts is the maximum number of acceptable messages from a signer within a slot & round.
func maxMessageCounts(committeeSize int) MessageCounts {
	return MessageCounts{
		PreConsensus:  1,
		Proposal:      1,
		Prepare:       1,
		Commit:        1,
		Decided:       committeeSize + 1,
		RoundChange:   1,
		PostConsensus: 1,
	}
}

type MessageCounts struct {
	PreConsensus  int
	Proposal      int
	Prepare       int
	Commit        int
	Decided       int
	RoundChange   int
	PostConsensus int
}

func (c *MessageCounts) Validate(msg *queue.DecodedSSVMessage) error {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			if c.Proposal > 0 || c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = "proposal"
				return err
			}
		case specqbft.PrepareMsgType:
			if c.Prepare > 0 || c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = "prepare"
				return err
			}
		case specqbft.CommitMsgType:
			if c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = "commit"
				return err
			}
		case specqbft.RoundChangeMsgType:
			if c.RoundChange > 0 {
				err := ErrUnexpectedMessageType
				err.got = "round change"
				return err
			}
		}
	case *spectypes.SignedPartialSignatureMessage:
		switch m.Message.Type {
		case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig:
			if c.PreConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = "pre-consensus"
				return err
			}
		case spectypes.PostConsensusPartialSig:
			if c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = "post-consensus"
				return err
			}
		default:
			// TODO: handle
		}
	//TODO: other cases
	default:
		return fmt.Errorf("unexpected message type: %d", m)
	}

	return nil
}

func (c *MessageCounts) Record(msg *queue.DecodedSSVMessage) {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			c.Proposal++
		case specqbft.PrepareMsgType:
			c.Prepare++
		case specqbft.CommitMsgType:
			if l := len(msg.Body.(*specqbft.SignedMessage).Signers); l == 1 {
				c.Commit++
			} else if l > 1 {
				c.Decided++
			} else {
				// TODO: panic because 0-length signers should be checked before
			}
		case specqbft.RoundChangeMsgType:
			c.RoundChange++
		}
	case *spectypes.SignedPartialSignatureMessage:
		switch m.Message.Type {
		case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig:
			c.PreConsensus++
		case spectypes.PostConsensusPartialSig:
			c.PostConsensus++
		default:
			// TODO: handle
		}
		//TODO: other cases
	default:
		panic("unexpected message type")
	}
}

func (c *MessageCounts) Exceeds(limits MessageCounts) bool {
	return c.PreConsensus > limits.PreConsensus ||
		c.Proposal > limits.Proposal ||
		c.Prepare > limits.Prepare ||
		c.Commit > limits.Commit ||
		c.Decided > limits.Decided ||
		c.RoundChange > limits.RoundChange ||
		c.PostConsensus > limits.PostConsensus
}
