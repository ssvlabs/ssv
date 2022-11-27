package storage

import (
	"github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"sync"
)

// QBFTSyncMap wrap sync map with cast functions
type QBFTSyncMap struct {
	m sync.Map
}

// Get store from sync map by role type
func (m *QBFTSyncMap) Get(role types.BeaconRole) qbftstorage.QBFTStore {
	s, ok := m.m.Load(role)
	if ok {
		qbftStorage := s.(qbftstorage.QBFTStore)
		return qbftStorage
	}
	return nil
}

// Add store to sync map by role as a key
func (m *QBFTSyncMap) Add(role types.BeaconRole, store qbftstorage.QBFTStore) {
	m.m.Store(role, store)
}
