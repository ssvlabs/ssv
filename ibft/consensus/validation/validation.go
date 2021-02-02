package validation

import "github.com/bloxapp/ssv/ibft/consensus"

// validationConsensus implements consensus.Consensus interface
type validationConsensus struct {
}

// New is the constructor of validationConsensus
func New() consensus.Consensus {
	return &validationConsensus{}
}

func (c *validationConsensus) ValidateValue(value []byte) error {
	// TODO: Implement
	return nil
}
