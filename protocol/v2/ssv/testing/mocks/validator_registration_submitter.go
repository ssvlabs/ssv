package mocks

import (
	"context"

	"github.com/attestantio/go-eth2-client/api"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type ValidatorRegistrationSubmitter struct {
	b beacon.BeaconNode
}

func NewValidatorRegistrationSubmitter(b beacon.BeaconNode) *ValidatorRegistrationSubmitter {
	return &ValidatorRegistrationSubmitter{
		b: b,
	}
}

func (m *ValidatorRegistrationSubmitter) Enqueue(registration *api.VersionedSignedValidatorRegistration) error {
	return m.b.SubmitValidatorRegistrations(context.Background(), []*api.VersionedSignedValidatorRegistration{registration})
}
