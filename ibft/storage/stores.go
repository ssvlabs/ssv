package storage

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// ParticipantStores wraps sync map with cast functions to qbft store
type ParticipantStores struct {
	m *hashmap.Map[spectypes.BeaconRole, ParticipantStore]
}

func NewStores() *ParticipantStores {
	return &ParticipantStores{
		m: hashmap.New[spectypes.BeaconRole, ParticipantStore](),
	}
}

// Get store from sync map by role type
func (qs *ParticipantStores) Get(role spectypes.BeaconRole) ParticipantStore {
	s, ok := qs.m.Get(role)
	if !ok {
		return nil
	}
	return s
}

// Add store to sync map by role as a key
func (qs *ParticipantStores) Add(role spectypes.BeaconRole, store ParticipantStore) {
	qs.m.Set(role, store)
}

// Each iterates over all stores and calls the given function
func (qs *ParticipantStores) Each(f func(role spectypes.BeaconRole, store ParticipantStore) error) error {
	var err error
	qs.m.Range(func(role spectypes.BeaconRole, store ParticipantStore) bool {
		err = f(role, store)
		return err == nil
	})
	return err
}
