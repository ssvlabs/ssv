package validatorstore

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/bloxapp/ssv/operator/operatordatastore"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
)

//go:generate mockgen -package=validatorstore -destination=./mock.go -source=./validator_store.go

type ValidatorStore interface {
	CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	AllActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	GetOperatorShares() []*types.SSVShare
	GetValidatorStats() storage.ValidatorStats
}

type validatorStore struct {
	storage.Shares
	*validatorsmap.ValidatorsMap
	operatordatastore.OperatorDataStore
}

func New(store storage.Shares, validatorsMap *validatorsmap.ValidatorsMap, operatorDataStore operatordatastore.OperatorDataStore) ValidatorStore {
	return &validatorStore{
		Shares:            store,
		ValidatorsMap:     validatorsMap,
		OperatorDataStore: operatorDataStore,
	}
}

func (vs *validatorStore) GetOperatorShares() []*types.SSVShare {
	return vs.Shares.GetOperatorShares(vs.GetOperatorData().ID)
}

// GetValidatorStats returns stats of validators, including the following:
//   - the amount of validators in the network
//   - the amount of active validators (i.e. not slashed or existed)
//   - the amount of validators assigned to this operator
func (vs *validatorStore) GetValidatorStats() storage.ValidatorStats {
	return vs.Shares.GetValidatorStats(vs.GetOperatorData().ID)
}
