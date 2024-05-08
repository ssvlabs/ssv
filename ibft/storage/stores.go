package storage

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/cornelk/hashmap"
)

// QBFTStores wraps sync map with cast functions to qbft store
type QBFTStores struct {
	m *hashmap.Map[spectypes.RunnerRole, qbftstorage.QBFTStore]
}

func NewStores() *QBFTStores {
	return &QBFTStores{
		m: hashmap.New[spectypes.RunnerRole, qbftstorage.QBFTStore](),
	}
}

func NewStoresFromRoles(db basedb.Database, roles ...spectypes.RunnerRole) *QBFTStores {
	stores := NewStores()
	for _, role := range roles {
		stores.Add(role, New(db, role.String()))
	}
	return stores
}

// Get store from sync map by role type
func (qs *QBFTStores) Get(role spectypes.RunnerRole) qbftstorage.QBFTStore {
	s, ok := qs.m.Get(role)
	if !ok {
		return nil
	}
	return s
}

// Add store to sync map by role as a key
func (qs *QBFTStores) Add(role spectypes.RunnerRole, store qbftstorage.QBFTStore) {
	qs.m.Set(role, store)
}

// Each iterates over all stores and calls the given function
func (qs *QBFTStores) Each(f func(role spectypes.RunnerRole, store qbftstorage.QBFTStore) error) error {
	var err error
	qs.m.Range(func(role spectypes.RunnerRole, store qbftstorage.QBFTStore) bool {
		err = f(role, store)
		return err == nil
	})
	return err
}
