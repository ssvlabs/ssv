package storage

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"

	"github.com/bloxapp/ssv/protocol/v2/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

type StoreRole string

const (
	StoreRoleAttester                  StoreRole = "ATTESTER"
	StoreRoleAggregator                StoreRole = "AGGREGATOR"
	StoreRoleProposer                  StoreRole = "PROPOSER"
	StoreRoleSyncCommittee             StoreRole = "SYNC_COMMITTEE"
	StoreRoleSyncCommitteeContribution StoreRole = "SYNC_COMMITTEE_CONTRIBUTION"
	StoreRoleValidatorRegistration     StoreRole = "VALIDATOR_REGISTRATION"
	StoreRoleVoluntaryExit             StoreRole = "VOLUNTARY_EXIT"
	StoreRoleCommittee                 StoreRole = "COMMITTEE"
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
		stores.Add(role, New(db, message.RunnerRoleToString(role)))
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

func StoreRoleFromBeaconRole(role spectypes.BeaconRole) StoreRole {
	switch role {
	case spectypes.BNRoleAttester:
		return StoreRoleAttester
	case spectypes.BNRoleAggregator:
		return StoreRoleAggregator
	case spectypes.BNRoleProposer:
		return StoreRoleProposer
	case spectypes.BNRoleSyncCommittee:
		return StoreRoleSyncCommittee
	case spectypes.BNRoleSyncCommitteeContribution:
		return StoreRoleSyncCommitteeContribution
	case spectypes.BNRoleValidatorRegistration:
		return StoreRoleValidatorRegistration
	case spectypes.BNRoleVoluntaryExit:
		return StoreRoleVoluntaryExit
	default:
		return ""
	}
}

func StoreRoleFromRunnerRole(role spectypes.RunnerRole) StoreRole {
	switch role {
	case spectypes.RoleAggregator:
		return StoreRoleAggregator
	case spectypes.RoleProposer:
		return StoreRoleProposer
	case spectypes.RoleSyncCommitteeContribution:
		return StoreRoleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		return StoreRoleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		return StoreRoleVoluntaryExit
	case spectypes.RoleCommittee:
		return StoreRoleCommittee
	default:
		return ""
	}
}
