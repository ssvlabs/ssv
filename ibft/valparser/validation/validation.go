package validation

import "github.com/bloxapp/ssv/ibft/valparser"

// validationConsensus implements valparser.ValueParser interface
type validationConsensus struct {
	inputVal *InputValue
}

// New is the constructor of validationConsensus
func New(inputVal *InputValue) valparser.ValueParser {
	return &validationConsensus{
		inputVal: inputVal,
	}
}

func (c *validationConsensus) ValidateValue(value []byte) error {
	// TODO: Implement
	return nil
}
