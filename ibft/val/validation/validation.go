package validation

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/val"
)

// validationConsensus implements val.ValueImplementation interface
type validationConsensus struct {
	logger   *zap.Logger
	inputVal *InputValue
}

// New is the constructor of validationConsensus
func New(logger *zap.Logger, inputVal *InputValue) val.ValueValidator {
	return &validationConsensus{
		logger:   logger,
		inputVal: inputVal,
	}
}

func (c *validationConsensus) Validate(value []byte) error {
	return nil
}
