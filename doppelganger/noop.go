package doppelganger

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type NoOpDoppelgangerProvider struct{}

func (NoOpDoppelgangerProvider) ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus {
	// Always allow signing
	return SigningEnabled
}

func (NoOpDoppelgangerProvider) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	// No operation
}

func (NoOpDoppelgangerProvider) StartMonitoring(ctx context.Context) {
	// No operation
}
