package validation

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

// maxMessageCounts is the maximum number of acceptable messages from a signer within a slot & round.
func maxMessageCounts(committeeSize int) MessageCounts {
	return MessageCounts{
		PreConsensus: 1,
		Proposals:    1,
		Prepares:     1,
		// TODO: max commits should adapt to the committeeSize.
		//     (see Gal's formula: https://hackmd.io/zT1hct3oRDW3QicByFkqsA)
		Commits:       0,
		PostConsensus: 1,
	}
}

type MessageCounts struct {
	PreConsensus  int
	Proposals     int
	Prepares      int
	Commits       int
	PostConsensus int
}

func (c *MessageCounts) Record(msg *queue.DecodedSSVMessage, limits MessageCounts) bool {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			c.Proposals++
		case specqbft.PrepareMsgType:
			c.Prepares++
		case specqbft.CommitMsgType:
			c.Commits++
		}
	case *spectypes.SignedPartialSignatureMessage:
		if m.Message.Type == spectypes.PostConsensusPartialSig {
			c.PostConsensus++
		} else {
			c.PreConsensus++
		}
	}
	return c.Exceeds(limits)
}

func (c *MessageCounts) Exceeds(limits MessageCounts) bool {
	return c.PreConsensus > limits.PreConsensus ||
		c.Proposals > limits.Proposals ||
		c.Prepares > limits.Prepares ||
		c.Commits > limits.Commits ||
		c.PostConsensus > limits.PostConsensus
}
