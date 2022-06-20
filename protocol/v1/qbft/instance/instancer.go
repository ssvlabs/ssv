package instance

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	qbft2 "github.com/bloxapp/ssv/spec/qbft"

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

// Result is a struct holding the result of a single iBFT instance
type Result struct {
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
	Containers() map[qbft2.MessageType]msgcont.MessageContainer
	ForceDecide(msg *message.SignedMessage)
	GetStageChan() chan qbft.RoundState
	CommittedAggregatedMsg() (*message.SignedMessage, error)
	GetCommittedAggSSVMessage() (message.SSVMessage, error)
	ProcessMsg(msg *message.SignedMessage) (bool, error)
	ResetRoundTimer()            // TODO temp solution for race condition with message process
	BroadcastChangeRound() error // TODO temp solution for race condition with message process
}
