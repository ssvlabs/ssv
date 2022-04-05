package instance

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"go.uber.org/zap"
)

// ControllerStartInstanceOptions defines type for Controller instance options
type ControllerStartInstanceOptions struct {
	Logger     *zap.Logger
	ValueCheck valcheck.ValueCheck
	SeqNumber  uint64
	Value      []byte
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
}

// InstanceResult is a struct holding the result of a single iBFT instance
type InstanceResult struct {
	Decided bool
	Msg     *proto.SignedMessage
}

// Instance represents an iBFT instance (a single sequence number)
type Instance interface {
	validation.Pipelines
	Init()
	Start(inputValue []byte) error
	Stop()
	State() *proto.State
	ForceDecide(msg *proto.SignedMessage)
	GetStageChan() chan proto.RoundState
	GetLastChangeRoundMsg() *proto.SignedMessage
	CommittedAggregatedMsg() (*proto.SignedMessage, error)
}
