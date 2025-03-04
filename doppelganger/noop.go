package doppelganger

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type NoOpHandler struct{}

func (NoOpHandler) Start(ctx context.Context) error {
	return nil
}

func (NoOpHandler) CanSign(validatorIndex phase0.ValidatorIndex) bool {
	return true
}

func (NoOpHandler) ReportQuorum(validatorIndex phase0.ValidatorIndex) {
	// No operation
}

func (NoOpHandler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	// No operation
}
