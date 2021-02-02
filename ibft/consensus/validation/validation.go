package validation

import (
	"encoding/json"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/consensus"
)

// validationConsensus implements consensus.Consensus interface
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
	// TODO: Implement
	actualData, err := json.Marshal(c.inputVal)
	if err != nil {
		return err
	}

	c.logger.Info("got validation request", zap.String("given_input", string(value)), zap.String("origin_input", string(actualData)))
	return nil
}
