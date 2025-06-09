package doppelganger

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type NoOpHandler struct{}

func (NoOpHandler) Start(context.Context) error {
	return nil
}

func (NoOpHandler) CanSign(phase0.ValidatorIndex) bool {
	return true
}

func (NoOpHandler) ReportQuorum(phase0.ValidatorIndex) {
	// No operation
}

func (NoOpHandler) RemoveValidatorState(phase0.ValidatorIndex) {
	// No operation
}
