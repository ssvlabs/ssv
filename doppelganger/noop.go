package doppelganger

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type NoOpDoppelgangerHandler struct{}

func (NoOpDoppelgangerHandler) Start(ctx context.Context) error {
	return nil
}

func (NoOpDoppelgangerHandler) CanSign(validatorIndex phase0.ValidatorIndex) bool {
	return true
}

func (NoOpDoppelgangerHandler) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	// No operation
}

func (NoOpDoppelgangerHandler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	// No operation
}
