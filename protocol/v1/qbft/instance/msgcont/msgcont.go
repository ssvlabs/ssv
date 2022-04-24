package msgcont

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// MessageContainer represents the behavior of the message container
type MessageContainer interface {
	// ReadOnlyMessagesByRound returns messages by the given round
	ReadOnlyMessagesByRound(round message.Round) []*message.SignedMessage

	// QuorumAchieved returns true if enough msgs were received (round, value)
	QuorumAchieved(round message.Round, value []byte) (bool, []*message.SignedMessage)

	PartialChangeRoundQuorum(stateRound message.Round) (found bool, lowestChangeRound message.Round)

	// AddMessage adds the given message to the container
	AddMessage(msg *message.SignedMessage, data []byte)

	// OverrideMessages will override all current msgs in container with the provided msg
	OverrideMessages(msg *message.SignedMessage, data []byte)
}
