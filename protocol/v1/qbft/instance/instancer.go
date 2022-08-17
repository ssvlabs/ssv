package instance

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// ControllerStartInstanceOptions defines type for Controller instance options
type ControllerStartInstanceOptions struct {
	Logger    *zap.Logger
	SeqNumber specqbft.Height
	Value     []byte
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
}

// Result is a struct holding the result of a single iBFT instance
type Result struct {
	Decided bool
	Msg     *specqbft.SignedMessage
}

// Instancer represents an iBFT instance (a single sequence number)
type Instancer interface {
	validation.Pipelines
	Init()
	Start(inputValue []byte) error
	Stop()
	State() *qbft.State
	Containers() map[specqbft.MessageType]msgcont.MessageContainer
	ForceDecide(msg *specqbft.SignedMessage)
	GetStageChan() chan qbft.RoundState
	CommittedAggregatedMsg() (*specqbft.SignedMessage, error)
	GetCommittedAggSSVMessage() (spectypes.SSVMessage, error)
	ProcessMsg(msg *specqbft.SignedMessage) (bool, error)
	ResetRoundTimer()            // TODO temp solution for race condition with message process
	BroadcastChangeRound() error // TODO temp solution for race condition with message process
}
