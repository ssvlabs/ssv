package validation

// message_counts.go contains code for counting and validating messages per validator-slot-round.

import (
	"fmt"
	"strings"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	preConsensusIdx = iota
	proposalIdx
	prepareIdx
	commitIdx
	roundChangeIdx
	postConsensusIdx
)

// SeenMsgTypes tracks whether various message types were received for validation.
// It stores them as a bitset. It is enough because limit of all messages is 1.
type SeenMsgTypes struct {
	msgBitMask uint8
	maxRCJ     uint8
}

// String provides a formatted representation of the SeenMsgTypes.
func (c *SeenMsgTypes) String() string {
	messageTypes := []string{
		"pre-consensus",
		"proposal",
		"prepare",
		"commit",
		"round change",
		"post-consensus",
	}

	getters := map[string]func() bool{
		messageTypes[0]: c.reachedPreConsensusLimit,
		messageTypes[1]: c.reachedProposalLimit,
		messageTypes[2]: c.reachedPrepareLimit,
		messageTypes[3]: c.reachedCommitLimit,
		messageTypes[4]: c.reachedRoundChangeLimit,
		messageTypes[5]: c.reachedPostConsensusLimit,
	}

	seen := make([]string, 0, len(getters))
	for _, mt := range messageTypes {
		if getters[mt]() {
			seen = append(seen, mt)
		}
	}

	return fmt.Sprintf("%s, maxRCJ=%d", strings.Join(seen, ", "), c.maxRCJ)
}

// ValidateConsensusMessage checks if the provided consensus message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func (c *SeenMsgTypes) ValidateConsensusMessage(signedSSVMessage *spectypes.SignedSSVMessage, msg *specqbft.Message) error {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		if c.reachedProposalLimit() {
			err := ErrDuplicatedMessage
			err.got = fmt.Sprintf("proposal, having %v", c.String())
			return err
		}
	case specqbft.PrepareMsgType:
		if c.reachedPrepareLimit() {
			err := ErrDuplicatedMessage
			err.got = fmt.Sprintf("prepare, having %v", c.String())
			return err
		}
	case specqbft.CommitMsgType:
		if c.reachedCommitLimit() && len(signedSSVMessage.OperatorIDs) == 1 {
			err := ErrDuplicatedMessage
			err.got = fmt.Sprintf("commit, having %v", c.String())
			return err
		}
	case specqbft.RoundChangeMsgType:
		if c.reachedRoundChangeLimit() {
			if uint8(len(msg.RoundChangeJustification)) == c.maxRCJ {
				err := ErrDuplicatedMessage
				err.got = fmt.Sprintf("round change with %d RCJ, having %v", len(msg.RoundChangeJustification), c.String())
				return err
			}

			if uint8(len(msg.RoundChangeJustification)) < c.maxRCJ {
				err := ErrRCLowerJustificationCount
				err.got = len(msg.RoundChangeJustification)
				err.want = fmt.Sprintf(">%d", c.maxRCJ)
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected signed message type") // should be checked before
	}

	return nil
}

// ValidatePartialSignatureMessage checks if the provided partial signature message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func (c *SeenMsgTypes) ValidatePartialSignatureMessage(m *spectypes.PartialSignatureMessages) error {
	switch m.Type {
	case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig:
		if c.reachedPreConsensusLimit() {
			err := ErrInvalidPartialSignatureTypeCount
			err.got = fmt.Sprintf("pre-consensus, having %v", c.String())
			return err
		}
	case spectypes.PostConsensusPartialSig:
		if c.reachedPostConsensusLimit() {
			err := ErrInvalidPartialSignatureTypeCount
			err.got = fmt.Sprintf("post-consensus, having %v", c.String())
			return err
		}
	default:
		return fmt.Errorf("unexpected partial signature message type") // should be checked before
	}

	return nil
}

// RecordConsensusMessage updates the counts based on the provided consensus message type.
func (c *SeenMsgTypes) RecordConsensusMessage(signedSSVMessage *spectypes.SignedSSVMessage, msg *specqbft.Message) error {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		c.recordProposal()
	case specqbft.PrepareMsgType:
		c.recordPrepare()
	case specqbft.CommitMsgType:
		if len(signedSSVMessage.OperatorIDs) == 1 {
			c.recordCommit()
		}
	case specqbft.RoundChangeMsgType:
		c.recordRoundChange(len(msg.RoundChangeJustification))
	default:
		return fmt.Errorf("unexpected signed message type") // should be checked before
	}
	return nil
}

// RecordPartialSignatureMessage updates the counts based on the provided partial signature message type.
func (c *SeenMsgTypes) RecordPartialSignatureMessage(messages *spectypes.PartialSignatureMessages) error {
	switch messages.Type {
	case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs, spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig:
		c.recordPreConsensus()
	case spectypes.PostConsensusPartialSig:
		c.recordPostConsensus()
	default:
		return fmt.Errorf("unexpected partial signature message type") // should be checked before
	}
	return nil
}

func (c *SeenMsgTypes) recordPreConsensus() {
	c.msgBitMask |= 1 << preConsensusIdx
}

func (c *SeenMsgTypes) recordProposal() {
	c.msgBitMask |= 1 << proposalIdx
}

func (c *SeenMsgTypes) recordPrepare() {
	c.msgBitMask |= 1 << prepareIdx
}

func (c *SeenMsgTypes) recordCommit() {
	c.msgBitMask |= 1 << commitIdx
}

func (c *SeenMsgTypes) recordRoundChange(justificationSize int) {
	c.msgBitMask |= 1 << roundChangeIdx
	// - maxRCJ never exceeds maxOperators
	// - maxRCJ always increases, otherwise the message is not accepted making this code unreachable
	c.maxRCJ = uint8(justificationSize)
}

func (c *SeenMsgTypes) recordPostConsensus() {
	c.msgBitMask |= 1 << postConsensusIdx
}

func (c *SeenMsgTypes) reachedPreConsensusLimit() bool {
	return (c.msgBitMask & (1 << preConsensusIdx)) != 0
}

func (c *SeenMsgTypes) reachedProposalLimit() bool {
	return (c.msgBitMask & (1 << proposalIdx)) != 0
}

func (c *SeenMsgTypes) reachedPrepareLimit() bool {
	return (c.msgBitMask & (1 << prepareIdx)) != 0
}

func (c *SeenMsgTypes) reachedCommitLimit() bool {
	return (c.msgBitMask & (1 << commitIdx)) != 0
}

func (c *SeenMsgTypes) reachedRoundChangeLimit() bool {
	return (c.msgBitMask & (1 << roundChangeIdx)) != 0
}

func (c *SeenMsgTypes) reachedPostConsensusLimit() bool {
	return (c.msgBitMask & (1 << postConsensusIdx)) != 0
}
