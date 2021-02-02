package validation

import "github.com/bloxapp/ssv/ibft/valueImpl"

// validationConsensus implements valueImpl.ValueImplementation interface
type validationConsensus struct {
	inputVal *InputValue
}

// New is the constructor of validationConsensus
func New(inputVal *InputValue) valueImpl.ValueImplementation {
	return &validationConsensus{
		inputVal: inputVal,
	}
}

func (c *validationConsensus) ValidateValue(value []byte) error {
	// TODO: Implement
	return nil
}
