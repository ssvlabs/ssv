package validation

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/consensus"
)

// validationConsensus implements consensus.ValueImplementation interface
type validationConsensus struct {
	logger   *zap.Logger
	inputVal *InputValue
}

// New is the constructor of validationConsensus
func New(logger *zap.Logger, inputVal *InputValue) consensus.Consensus {
	return &validationConsensus{
		logger:   logger,
		inputVal: inputVal,
	}
}

func (c *validationConsensus) ValidateValue(value []byte) error {
	return nil
}
