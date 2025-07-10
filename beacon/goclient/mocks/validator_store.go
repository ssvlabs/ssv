package mocks

import (
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type ValidatorStore struct{}

func NewValidatorStore() *ValidatorStore {
	return &ValidatorStore{}
}

func (m *ValidatorStore) SelfValidators() []*types.SSVShare {
	return nil
}
