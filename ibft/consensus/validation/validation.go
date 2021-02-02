package validation

import "github.com/bloxapp/ssv/ibft/consensus"

// validationConsensus implements consensus.Consensus interface
type validationConsensus struct {
	inputVal *InputValue
}

// New is the constructor of validationConsensus
func New(inputVal *InputValue) consensus.Consensus {
	return &validationConsensus{
		inputVal: inputVal,
	}
}

func (c *validationConsensus) ValidateValue(value []byte) error {
	// TODO: Implement
	return nil
}
