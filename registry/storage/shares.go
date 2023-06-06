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

var sharesPrefix = []byte("shares")

// SharesListFunc is a function that returns a filtered list of shares.
type SharesListFunc = func(filters ...func(share *types.SSVShare) bool) []*types.SSVShare

// Shares is the interface for managing shares.
type Shares interface {
	eth1.RegistryStore

	Get(pubKey []byte) *types.SSVShare
	List(filters ...func(*types.SSVShare) bool) []*types.SSVShare
	Save(logger *zap.Logger, shares ...*types.SSVShare) error
	Delete(pubKey []byte) error
}

type sharesStorage struct {
	db     basedb.IDb
	prefix []byte
	shares map[string]*types.SSVShare
	mu     sync.RWMutex
}

func NewSharesStorage(logger *zap.Logger, db basedb.IDb, prefix []byte) (Shares, error) {
	storage := &sharesStorage{
		shares: make(map[string]*types.SSVShare),
		db:     db,
		prefix: prefix,
	}
	err := storage.load(logger)
	if err != nil {
		return nil, err
	}
	return storage, nil
}

func (s *sharesStorage) load(logger *zap.Logger) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.GetAll(logger, append(s.prefix, sharesPrefix...), func(i int, obj basedb.Obj) error {
		val := &types.SSVShare{}
		if err := val.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize share: %w", err)
		}
		s.shares[hex.EncodeToString(val.ValidatorPubKey)] = val
		return nil
	})
}

func (s *sharesStorage) Get(pubKey []byte) *types.SSVShare {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.shares[hex.EncodeToString(pubKey)]
}

func (s *sharesStorage) List(filters ...func(*types.SSVShare) bool) []*types.SSVShare {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var shares []*types.SSVShare
	for _, share := range s.shares {
		match := true
		for _, filter := range filters {
			if !filter(share) {
				match = false
				break
			}
		}
		if match {
			shares = append(shares, share)
		}
	}
	return shares
}

func (s *sharesStorage) Save(logger *zap.Logger, shares ...*types.SSVShare) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, share := range shares {
		key := hex.EncodeToString(share.ValidatorPubKey)
		s.shares[key] = share
	}

	return s.db.SetMany(s.prefix, len(shares), func(i int) (basedb.Obj, error) {
		value, err := shares[i].Encode()
		if err != nil {
			logger.Error("failed to serialize share", zap.Error(err))
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: buildShareKey(shares[i].ValidatorPubKey), Value: value}, nil
	})
}

func (s *sharesStorage) Delete(pubKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.shares, hex.EncodeToString(pubKey))
	return s.db.Delete(s.prefix, buildShareKey(pubKey))
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *sharesStorage) UpdateValidatorMetadata(logger *zap.Logger, pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	key, err := hex.DecodeString(pk)
	if err != nil {
		return err
	}
	share := s.Get(key)
	if share == nil {
		return nil
	}
	share.BeaconMetadata = metadata
	return s.Save(logger, share)
}

// CleanRegistryData clears all registry data
func (s *sharesStorage) CleanRegistryData() error {
	return s.db.RemoveAllByCollection(sharesPrefix)
}

// buildShareKey builds share key using sharesPrefix & validator public key, e.g. "shares/0x00..01"
func buildShareKey(pk []byte) []byte {
	return bytes.Join([][]byte{sharesPrefix, pk}, []byte("/"))
}

// FilterOperatorID filters by operator ID.
func FilterOperatorID(operatorID spectypes.OperatorID) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return share.BelongsToOperator(operatorID)
	}
}

// FilterNotLiquidated filters for not liquidated.
func FilterNotLiquidated() func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return !share.Liquidated
	}
}

// FilterActiveValidator filters for active validators.
func FilterActiveValidator() func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		return share.HasBeaconMetadata()
	}
}

// FilterByClusterID filters by cluster id.
func FilterByClusterID(clusterID []byte) func(share *types.SSVShare) bool {
	return func(share *types.SSVShare) bool {
		var operatorIDs []uint64
		for _, op := range share.Committee {
			operatorIDs = append(operatorIDs, op.OperatorID)
		}

		shareClusterID, _ := types.ComputeClusterIDHash(share.OwnerAddress.Bytes(), operatorIDs)
		return bytes.Equal(shareClusterID, clusterID)
	}
}
