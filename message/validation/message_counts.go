package validation

// message_counts.go contains code for counting and validating messages per validator-slot-round.

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// MessageCounts tracks the number of various message types received for validation.
type MessageCounts struct {
	PreConsensus   int
	Proposal       int
	Prepare        int
	Commit         int
	Decided        int
	RoundChange    int
	RoundChangeMsg queue.DecodedSSVMessage // TODO: delete it, it was added for debugging
	PostConsensus  int
}

// String provides a formatted representation of the MessageCounts.
func (c *MessageCounts) String() string {
	return fmt.Sprintf("pre-consensus: %v, proposal: %v, prepare: %v, commit: %v, decided: %v, round change: %v, post-consensus: %v",
		c.PreConsensus,
		c.Proposal,
		c.Prepare,
		c.Commit,
		c.Decided,
		c.RoundChange,
		c.PostConsensus,
	)
}

// ValidateConsensusMessage checks if the provided consensus message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func (c *MessageCounts) ValidateConsensusMessage(signedSSVMessage *spectypes.SignedSSVMessage, msg *specqbft.Message, limits MessageCounts) error {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		if c.Proposal >= limits.Proposal {
			err := ErrTooManySameTypeMessagesPerRound
			err.got = fmt.Sprintf("proposal, having %v", c.String())
			return err
		}
	case specqbft.PrepareMsgType:
		if c.Prepare >= limits.Prepare {
			err := ErrTooManySameTypeMessagesPerRound
			err.got = fmt.Sprintf("prepare, having %v", c.String())
			return err
		}
	case specqbft.CommitMsgType:
		if len(signedSSVMessage.GetOperatorIDs()) == 1 {
			if c.Commit >= limits.Commit {
				err := ErrTooManySameTypeMessagesPerRound
				err.got = fmt.Sprintf("commit, having %v", c.String())
				return err
			}
		}
		if len(signedSSVMessage.GetOperatorIDs()) > 1 {
			if c.Decided >= limits.Decided {
				err := ErrTooManySameTypeMessagesPerRound
				err.got = fmt.Sprintf("decided, having %v", c.String())
				return err
			}
		}
	case specqbft.RoundChangeMsgType:
		if c.RoundChange >= limits.RoundChange {
			err := ErrTooManySameTypeMessagesPerRound

			err.got = fmt.Sprintf("round change, having %v", c.String())
			return err
		}
	default:
		panic("unexpected signed message type") // should be checked before
	}

	return nil
}

// ValidatePartialSignatureMessage checks if the provided partial signature message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func (c *MessageCounts) ValidatePartialSignatureMessage(m *spectypes.PartialSignatureMessages, limits MessageCounts) error {
	switch m.Type {
	case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig:
		if c.PreConsensus > limits.PreConsensus {
			err := ErrTooManySameTypeMessagesPerRound
			err.got = fmt.Sprintf("pre-consensus, having %v", c.String())
			return err
		}
	case spectypes.PostConsensusPartialSig:
		if c.PostConsensus > limits.PostConsensus {
			err := ErrTooManySameTypeMessagesPerRound
			err.got = fmt.Sprintf("post-consensus, having %v", c.String())
			return err
		}
	default:
		panic("unexpected partial signature message type") // should be checked before
	}

	return nil
}

// RecordConsensusMessage updates the counts based on the provided consensus message type.
func (c *MessageCounts) RecordConsensusMessage(signedSSVMessage *spectypes.SignedSSVMessage, msg *specqbft.Message) {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		c.Proposal++
	case specqbft.PrepareMsgType:
		c.Prepare++
	case specqbft.CommitMsgType:
		switch {
		case len(signedSSVMessage.GetOperatorIDs()) == 1:
			c.Commit++
		case len(signedSSVMessage.GetOperatorIDs()) > 1:
			c.Decided++
		default:
			panic("unexpected signers length 0") // 0 length should be checked before
		}
	case specqbft.RoundChangeMsgType:
		c.RoundChange++
	default:
		panic("unexpected signed message type") // should be checked before
	}
}

// RecordPartialSignatureMessage updates the counts based on the provided partial signature message type.
func (c *MessageCounts) RecordPartialSignatureMessage(messages *spectypes.PartialSignatureMessages) {
	switch messages.Type {
	case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig:
		c.PreConsensus++
	case spectypes.PostConsensusPartialSig:
		c.PostConsensus++
	default:
		panic("unexpected partial signature message type") // should be checked before
	}
}

// maxMessageCounts is the maximum number of acceptable messages from a signer within a slot & round.
func maxMessageCounts(committeeSize int) MessageCounts {
	maxDecided := maxDecidedCount(committeeSize)

	return MessageCounts{
		PreConsensus:  1,
		Proposal:      1,
		Prepare:       1,
		Commit:        1,
		Decided:       maxDecided,
		RoundChange:   1,
		PostConsensus: 1,
	}
}

func maxDecidedCount(committeeSize int) int {
	f := (committeeSize - 1) / 3
	return committeeSize * (f + 1) // N * (f + 1)
}
