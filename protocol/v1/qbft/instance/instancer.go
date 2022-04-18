package instance

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"

	"go.uber.org/zap"
)

// ControllerStartInstanceOptions defines type for Controller instance options
type ControllerStartInstanceOptions struct {
	Logger    *zap.Logger
	SeqNumber message.Height
	Value     []byte
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
}

// InstanceResult is a struct holding the result of a single iBFT instance
type InstanceResult struct {
	Decided bool
	Msg     *message.SignedMessage
}

// Instancer represents an iBFT instance (a single sequence number)
type Instancer interface {
	validation.Pipelines
	Init()
	Start(inputValue []byte) error
	Stop()
	State() *qbft.State
	ForceDecide(msg *message.SignedMessage)
	GetStageChan() chan qbft.RoundState
	GetLastChangeRoundMsg() *message.SignedMessage
	CommittedAggregatedMsg() (*message.SignedMessage, error)
	GetCommittedAggSSVMessage() (message.SSVMessage, error)
	ProcessMsg(msg *message.SignedMessage) (bool, []byte, error)
	ResetRoundTimer()            // TODO temp solution for race condition with message process
	BroadcastChangeRound() error // TODO temp solution for race condition with message process
}
