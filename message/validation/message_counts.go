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
		Proposals:     1,
		Prepares:      1,
		Commits:       1,
		Decided:       committeeSize + 1,
		PostConsensus: 1,
	}
}

type MessageCounts struct {
	PreConsensus  int
	Proposals     int
	Prepares      int
	Commits       int
	Decided       int
	PostConsensus int
}

func (c *MessageCounts) Validate(msg *queue.DecodedSSVMessage) error {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			if c.Proposals > 0 || c.Commits > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				return fmt.Errorf("proposal is not expected")
			}
		case specqbft.PrepareMsgType:
			if c.Prepares > 0 || c.Commits > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				return fmt.Errorf("prepare is not expected")
			}
		case specqbft.CommitMsgType:
			if c.Commits > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				return fmt.Errorf("commit is not expected")
			}
		}
	case *spectypes.PartialSigMsgType:
		if c.PostConsensus > 0 { // TODO: do we need this check?
			return fmt.Errorf("post-consensus is not expected")
		}
	//TODO: other cases
	default:
		return fmt.Errorf("unexpected message type: %")
	}

	return nil
}

func (c *MessageCounts) Record(msg *queue.DecodedSSVMessage) {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			c.Proposals++
		case specqbft.PrepareMsgType:
			c.Prepares++
		case specqbft.CommitMsgType:
			if l := len(msg.Body.(*specqbft.SignedMessage).Signers); l == 1 {
				c.Commits++
			} else if l > 1 {
				c.Decided++
			} else {
				// TODO: panic because 0-length signers should be checked before
			}
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
	}
}

func (c *MessageCounts) Exceeds(limits MessageCounts) bool {
	return c.PreConsensus > limits.PreConsensus ||
		c.Proposals > limits.Proposals ||
		c.Prepares > limits.Prepares ||
		c.Commits > limits.Commits ||
		c.Decided > limits.Decided ||
		c.PostConsensus > limits.PostConsensus
}
