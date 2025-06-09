package mocks

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type ValidatorStore struct{}

func NewValidatorStore() *ValidatorStore {
	return &ValidatorStore{}
}

func (m *ValidatorStore) SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare {
	return nil
}
