package doppelganger

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type NoOpDoppelgangerHandler struct{}

func (NoOpDoppelgangerHandler) ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus {
	// Always allow signing
	return SigningEnabled
}

func (NoOpDoppelgangerHandler) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	// No operation
}

func (NoOpDoppelgangerHandler) Start(ctx context.Context) error {
	return nil
}

func (NoOpDoppelgangerHandler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	// No operation
}
