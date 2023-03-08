package validator

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(logger *zap.Logger, share *types.SSVShare) error
	SaveValidatorShares(logger *zap.Logger, shares []*types.SSVShare) error
	GetValidatorShare(key []byte) (*types.SSVShare, bool, error)
	GetAllValidatorShares(logger *zap.Logger) ([]*types.SSVShare, error)
	GetFilteredValidatorShares(logger *zap.Logger, f func(share *types.SSVShare) bool) ([]*types.SSVShare, error)
	DeleteValidatorShare(key []byte) error
}

func collectionPrefix() []byte {
	return []byte("share-")
}

// CollectionOptions struct
type CollectionOptions struct {
	DB basedb.IDb
}

// Collection struct
type Collection struct {
	db   basedb.IDb
	lock sync.RWMutex
}

// NewCollection creates new share storage
func NewCollection(options CollectionOptions) ICollection {
	collection := Collection{
		db:   options.DB,
		lock: sync.RWMutex{},
	}
	return &collection
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(logger *zap.Logger, share *types.SSVShare) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(logger, share)
	if err != nil {
		return err
	}
	return nil
}

func (s *Collection) saveUnsafe(logger *zap.Logger, share *types.SSVShare) error {
	value, err := share.Encode()
	if err != nil {
		logger.Error("failed to serialize share", zap.Error(err))
		return err
	}
	return s.db.Set(collectionPrefix(), share.ValidatorPubKey, value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*types.SSVShare, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(key)
}

func (s *Collection) getUnsafe(key []byte) (*types.SSVShare, bool, error) {
	obj, found, err := s.db.Get(collectionPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	value := &types.SSVShare{}
	err = value.Decode(obj.Value)
	return value, found, err
}

func (s *Collection) SaveValidatorShares(logger *zap.Logger, shares []*types.SSVShare) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.SetMany(collectionPrefix(), len(shares), func(i int) (basedb.Obj, error) {
		value, err := shares[i].Encode()
		if err != nil {
			logger.Error("failed to serialize share", zap.Error(err))
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: shares[i].ValidatorPubKey, Value: value}, nil
	})
}

// CleanRegistryData clears all registry data
func (s *Collection) CleanRegistryData() error {
	return s.cleanAllShares()
}

func (s *Collection) cleanAllShares() error {
	return s.db.RemoveAllByCollection(collectionPrefix())
}

// GetAllValidatorShares returns all shares
func (s *Collection) GetAllValidatorShares(logger *zap.Logger) ([]*types.SSVShare, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.SSVShare

	err := s.db.GetAll(logger, collectionPrefix(), func(i int, obj basedb.Obj) error {
		val := &types.SSVShare{}
		if err := val.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize share: %w", err)
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// ByOperatorID filters by operator ID.
func ByOperatorID(operatorID spectypes.OperatorID) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return share.BelongsToOperator(operatorID)
	}
}

// NotLiquidated filters not liquidated and by operator public key.
func NotLiquidated() func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return !share.Liquidated
	}
}

// ByOperatorIDAndNotLiquidated filters not liquidated and by operator ID.
func ByOperatorIDAndNotLiquidated(operatorID spectypes.OperatorID) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return share.BelongsToOperator(operatorID) && !share.Liquidated
	}
}

// ByClusterID filters by cluster id.
func ByClusterID(clusterID []byte) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return bytes.Equal(share.ClusterID, clusterID)
	}
}

// ByOwnerAddress filters by owner address.
func ByOwnerAddress(ownerAddress common.Address) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return bytes.Equal(share.OwnerAddress.Bytes(), ownerAddress.Bytes())
	}
}

// GetFilteredValidatorShares returns shares by a filter.
func (s *Collection) GetFilteredValidatorShares(logger *zap.Logger, filter func(share *types.SSVShare) bool) ([]*types.SSVShare, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.SSVShare

	err := s.db.GetAll(logger, collectionPrefix(), func(i int, obj basedb.Obj) error {
		share := &types.SSVShare{}
		if err := share.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize validator: %w", err)
		}
		if filter(share) {
			res = append(res, share)
		}
		return nil
	})

	return res, err
}

// DeleteValidatorShare removes validator share by key
func (s *Collection) DeleteValidatorShare(key []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.Delete(collectionPrefix(), key); err != nil {
		return fmt.Errorf("delete share: %w", err)
	}

	return nil
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *Collection) UpdateValidatorMetadata(logger *zap.Logger, pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key, err := hex.DecodeString(pk)
	if err != nil {
		return err
	}
	share, found, err := s.getUnsafe(key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	share.BeaconMetadata = metadata
	return s.saveUnsafe(logger, share)
}
