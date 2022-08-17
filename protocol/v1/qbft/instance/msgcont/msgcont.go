package msgcont

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

// MessageContainer represents the behavior of the message container
type MessageContainer interface {
	// ReadOnlyMessagesByRound returns messages by the given round
	ReadOnlyMessagesByRound(round specqbft.Round) []*specqbft.SignedMessage

	//AllMessaged return all msg's
	AllMessaged(func(round specqbft.Round, msg *specqbft.SignedMessage)) []*specqbft.SignedMessage

	// QuorumAchieved returns true if enough msgs were received (round, value)
	QuorumAchieved(round specqbft.Round, value []byte) (bool, []*specqbft.SignedMessage)

	PartialChangeRoundQuorum(stateRound specqbft.Round) (found bool, lowestChangeRound specqbft.Round)

	// AddMessage adds the given message to the container
	AddMessage(msg *specqbft.SignedMessage, data []byte)

	// OverrideMessages will override all current msgs in container with the provided msg
	OverrideMessages(msg *specqbft.SignedMessage, data []byte)
}
