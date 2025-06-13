package storage

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// ParticipantStores wraps sync map with cast functions to qbft store
type ParticipantStores struct {
	m *hashmap.Map[spectypes.BeaconRole, qbftstorage.ParticipantStore]
}

func NewStores() *ParticipantStores {
	return &ParticipantStores{
		m: hashmap.New[spectypes.BeaconRole, qbftstorage.ParticipantStore](),
	}
}

func NewStoresFromRoles(logger *zap.Logger, netCfg networkconfig.NetworkConfig, db basedb.Database, slotTickerProvider slotticker.Provider, roles ...spectypes.BeaconRole) *ParticipantStores {
	stores := NewStores()
	for _, role := range roles {
		stores.Add(role, New(logger, db, role, netCfg, slotTickerProvider))
	}
	return stores
}

// Get store from sync map by role type
func (qs *ParticipantStores) Get(role spectypes.BeaconRole) qbftstorage.ParticipantStore {
	s, ok := qs.m.Get(role)
	if !ok {
		return nil
	}
	return s
}

// Add store to sync map by role as a key
func (qs *ParticipantStores) Add(role spectypes.BeaconRole, store qbftstorage.ParticipantStore) {
	qs.m.Set(role, store)
}

// Each iterates over all stores and calls the given function
func (qs *ParticipantStores) Each(f func(role spectypes.BeaconRole, store qbftstorage.ParticipantStore) error) error {
	var err error
	qs.m.Range(func(role spectypes.BeaconRole, store qbftstorage.ParticipantStore) bool {
		err = f(role, store)
		return err == nil
	})
	return err
}
