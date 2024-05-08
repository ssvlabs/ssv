package storage

import (
	genesisqbftstorage "github.com/bloxapp/ssv/protocol/v2/genesisqbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/cornelk/hashmap"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

// QBFTStores wraps sync map with cast functions to qbft store
type QBFTStores struct {
	m *hashmap.Map[genesisspectypes.BeaconRole, genesisqbftstorage.QBFTStore]
}

func NewStores() *QBFTStores {
	return &QBFTStores{
		m: hashmap.New[genesisspectypes.BeaconRole, genesisqbftstorage.QBFTStore](),
	}
}

func NewStoresFromRoles(db basedb.Database, roles ...genesisspectypes.BeaconRole) *QBFTStores {
	stores := NewStores()
	for _, role := range roles {
		stores.Add(role, New(db, role.String()))
	}
	return stores
}

// Get store from sync map by role type
func (qs *QBFTStores) Get(role genesisspectypes.BeaconRole) genesisqbftstorage.QBFTStore {
	s, ok := qs.m.Get(role)
	if !ok {
		return nil
	}
	return s
}

// Add store to sync map by role as a key
func (qs *QBFTStores) Add(role genesisspectypes.BeaconRole, store genesisqbftstorage.QBFTStore) {
	qs.m.Set(role, store)
}

// Each iterates over all stores and calls the given function
func (qs *QBFTStores) Each(f func(role genesisspectypes.BeaconRole, store genesisqbftstorage.QBFTStore) error) error {
	var err error
	qs.m.Range(func(role genesisspectypes.BeaconRole, store genesisqbftstorage.QBFTStore) bool {
		err = f(role, store)
		return err == nil
	})
	return err
}
