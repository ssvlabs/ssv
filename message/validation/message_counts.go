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

func (c *MessageCounts) Validate(msg *queue.DecodedSSVMessage) error {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			if c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = fmt.Sprintf("proposal, having %v", c.String())
				return err
			}
		case specqbft.PrepareMsgType:
			if c.Prepare > 0 || c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				if c.Prepare > 0 {
					err.reject = true
				}
				err.got = fmt.Sprintf("prepare, having %v", c.String())
				return err
			}
		case specqbft.CommitMsgType:
			if len(m.Signers) == 1 && c.Commit > 0 || c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				if c.Commit > 0 {
					err.reject = true
				}
				err.got = fmt.Sprintf("commit, having %v", c.String())
				return err
			}
			if len(m.Signers) > 1 && c.Decided > 0 || c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = fmt.Sprintf("decided, having %v", c.String())
				return err
			}
		case specqbft.RoundChangeMsgType:
			if c.RoundChange > 0 {
				err := ErrUnexpectedMessageType
				if c.RoundChange > 0 {
					err.reject = true
				}
				err.got = fmt.Sprintf("round change, having %v", c.String())
				return err
			}
		default:
			panic("unexpected signed message type") // should be checked before
		}
	case *spectypes.SignedPartialSignatureMessage:
		switch m.Message.Type {
		case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig:
			if c.PreConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = fmt.Sprintf("pre-consensus, having %v", c.String())
				return err
			}
		case spectypes.PostConsensusPartialSig:
			if c.PostConsensus > 0 {
				err := ErrUnexpectedMessageType
				err.got = fmt.Sprintf("post-consensus, having %v", c.String())
				return err
			}
		default:
			panic("unexpected partial signature message type") // should be checked before
		}
	default:
		panic("unexpected ssv message type") // should be checked before
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
			if len(m.Signers) == 1 {
				c.Commit++
			} else if len(m.Signers) > 1 {
				c.Decided++
			} else {
				panic("expected signers") // 0 length should be checked before
			}
		case specqbft.RoundChangeMsgType:
			c.RoundChange++
		default:
			panic("unexpected signed message type") // should be checked before
		}
	case *spectypes.SignedPartialSignatureMessage:
		switch m.Message.Type {
		case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig:
			c.PreConsensus++
		case spectypes.PostConsensusPartialSig:
			c.PostConsensus++
		default:
			panic("unexpected partial signature message type") // should be checked before
		}
	default:
		panic("unexpected ssv message type")
	}
}

func (c *MessageCounts) ReachedLimits(limits MessageCounts) bool {
	return c.PreConsensus >= limits.PreConsensus ||
		c.Proposal >= limits.Proposal ||
		c.Prepare >= limits.Prepare ||
		c.Commit >= limits.Commit ||
		c.Decided >= limits.Decided ||
		c.RoundChange >= limits.RoundChange ||
		c.PostConsensus >= limits.PostConsensus
}
