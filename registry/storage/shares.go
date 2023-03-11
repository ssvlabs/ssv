package storage

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	sharesPrefix = []byte("shares")
)

// FilteredSharesFunc is a function that returns filtered shares
type FilteredSharesFunc = func(logger *zap.Logger, f func(share *types.SSVShare) bool) ([]*types.SSVShare, error)

// Shares is the interface for managing shares
type Shares interface {
	eth1.RegistryStore

	SaveShare(logger *zap.Logger, share *types.SSVShare) error
	SaveShareMany(logger *zap.Logger, shares []*types.SSVShare) error
	GetShare(key []byte) (*types.SSVShare, bool, error)
	GetAllShares(logger *zap.Logger) ([]*types.SSVShare, error)
	GetFilteredShares(logger *zap.Logger, f func(share *types.SSVShare) bool) ([]*types.SSVShare, error)
	DeleteShare(key []byte) error
}

type sharesStorage struct {
	db     basedb.IDb
	lock   sync.RWMutex
	prefix []byte
}

// NewSharesStorage creates new share storage
func NewSharesStorage(db basedb.IDb, prefix []byte) Shares {
	return &sharesStorage{
		db:     db,
		lock:   sync.RWMutex{},
		prefix: prefix,
	}
}

// SaveShare validator share to db
func (s *sharesStorage) SaveShare(logger *zap.Logger, share *types.SSVShare) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(logger, share)
	if err != nil {
		return err
	}
	return nil
}

func (s *sharesStorage) saveUnsafe(logger *zap.Logger, share *types.SSVShare) error {
	value, err := share.Encode()
	if err != nil {
		logger.Error("failed to serialize share", zap.Error(err))
		return err
	}
	return s.db.Set(s.prefix, buildShareKey(share.ValidatorPubKey), value)
}

// GetShare by key
func (s *sharesStorage) GetShare(key []byte) (*types.SSVShare, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(key)
}

func (s *sharesStorage) getUnsafe(key []byte) (*types.SSVShare, bool, error) {
	obj, found, err := s.db.Get(s.prefix, buildShareKey(key))
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	value := &types.SSVShare{}
	err = value.Decode(obj.Value)
	return value, found, err
}

// SaveShareMany saves many shares
func (s *sharesStorage) SaveShareMany(logger *zap.Logger, shares []*types.SSVShare) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.SetMany(s.prefix, len(shares), func(i int) (basedb.Obj, error) {
		value, err := shares[i].Encode()
		if err != nil {
			logger.Error("failed to serialize share", zap.Error(err))
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: buildShareKey(shares[i].ValidatorPubKey), Value: value}, nil
	})
}

// CleanRegistryData clears all registry data
func (s *sharesStorage) CleanRegistryData() error {
	return s.cleanAllShares()
}

func (s *sharesStorage) cleanAllShares() error {
	return s.db.RemoveAllByCollection(sharesPrefix)
}

// GetAllShares returns all shares
func (s *sharesStorage) GetAllShares(logger *zap.Logger) ([]*types.SSVShare, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.SSVShare

	err := s.db.GetAll(logger, append(s.prefix, sharesPrefix...), func(i int, obj basedb.Obj) error {
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

// ByOperatorIDAndActive filters not liquidated by operator ID and has beacon metadata.
func ByOperatorIDAndActive(operatorID spectypes.OperatorID) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return share.HasBeaconMetadata() && !share.Liquidated && share.BelongsToOperator(operatorID)
	}
}

// ByClusterID filters by cluster id.
func ByClusterID(clusterID []byte) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		var operatorIDs []uint64
		for _, op := range share.Committee {
			operatorIDs = append(operatorIDs, uint64(op.OperatorID))
		}

		shareClusterID, _ := types.ComputeClusterIDHash(share.OwnerAddress.Bytes(), operatorIDs)
		return bytes.Equal(shareClusterID, clusterID)
	}
}

// GetFilteredShares returns shares by a filter.
func (s *sharesStorage) GetFilteredShares(logger *zap.Logger, filter func(share *types.SSVShare) bool) ([]*types.SSVShare, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.SSVShare

	err := s.db.GetAll(logger, append(s.prefix, sharesPrefix...), func(i int, obj basedb.Obj) error {
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

// DeleteShare removes validator share by key
func (s *sharesStorage) DeleteShare(key []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.Delete(s.prefix, buildShareKey(key)); err != nil {
		return fmt.Errorf("delete share: %w", err)
	}

	return nil
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *sharesStorage) UpdateValidatorMetadata(logger *zap.Logger, pk string, metadata *beaconprotocol.ValidatorMetadata) error {
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

// buildShareKey builds share key using sharesPrefix & validator public key, e.g. "shares/0x00..01"
func buildShareKey(pk []byte) []byte {
	return bytes.Join([][]byte{sharesPrefix, pk}, []byte("/"))
}
