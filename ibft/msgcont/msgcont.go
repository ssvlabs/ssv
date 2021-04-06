package msgcont

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

// MessageContainer represents the behavior of the message container
type MessageContainer interface {
	// ReadOnlyMessagesByRound returns messages by the given round
	ReadOnlyMessagesByRound(round uint64) []*proto.SignedMessage

	// QuorumAchieved returns true if enough msgs were received (round, value)
	QuorumAchieved(round uint64, value []byte) (bool, []*proto.SignedMessage)

	// AddMessage adds the given message to the container
	AddMessage(msg *proto.SignedMessage)
}
