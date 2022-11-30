package storage

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"sync"
)

// QBFTStores wraps sync map with cast functions to qbft store
type QBFTStores struct {
	m sync.Map
}

func NewStores() *QBFTStores {
	return &QBFTStores{
		m: sync.Map{},
	}
}

// Get store from sync map by role type
func (qs *QBFTStores) Get(role types.BeaconRole) qbftstorage.QBFTStore {
	s, ok := qs.m.Load(role)
	if ok {
		qbftStorage := s.(qbftstorage.QBFTStore)
		return qbftStorage
	}
	return nil
}

// Add store to sync map by role as a key
func (qs *QBFTStores) Add(role types.BeaconRole, store qbftstorage.QBFTStore) {
	qs.m.Store(role, store)
}
