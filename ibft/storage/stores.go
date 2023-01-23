package storage

import (
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
)

func NewStoresFromRoles(db basedb.IDb, logger *zap.Logger, roles ...spectypes.BeaconRole) *QBFTStores {
	stores := NewStores()

	for _, role := range roles {
		stores.Add(role, New(db, logger, role.String(), forksprotocol.GenesisForkVersion))
	}

	return stores
}

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
func (qs *QBFTStores) Get(role spectypes.BeaconRole) qbftstorage.QBFTStore {
	s, ok := qs.m.Load(role)
	if ok {
		qbftStorage := s.(qbftstorage.QBFTStore)
		return qbftStorage
	}
	return nil
}

// Add store to sync map by role as a key
func (qs *QBFTStores) Add(role spectypes.BeaconRole, store qbftstorage.QBFTStore) {
	qs.m.Store(role, store)
}
