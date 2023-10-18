package storage

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
)

var sharesPrefix = []byte("shares")

// SharesFilter is a function that filters shares.
type SharesFilter func(*types.SSVShare) bool

// SharesListFunc is a function that returns a filtered list of shares.
type SharesListFunc = func(filters ...SharesFilter) []*types.SSVShare

// Shares is the interface for managing shares.
type Shares interface {
	// Get returns the share for the given public key, or nil if not found.
	Get(txn basedb.Reader, pubKey []byte) *types.SSVShare

	// List returns a list of shares, filtered by the given filters (if any).
	List(txn basedb.Reader, filters ...SharesFilter) []*types.SSVShare

	// Save saves the given shares.
	Save(txn basedb.ReadWriter, shares ...*types.SSVShare) error

	// Delete deletes the share for the given public key.
	Delete(txn basedb.ReadWriter, pubKey []byte) error

	// Drop deletes all shares.
	Drop() error

	// UpdateValidatorMetadata updates validator metadata.
	UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error
}

type sharesStorage struct {
	logger *zap.Logger
	db     basedb.Database
	prefix []byte
	shares map[string]*types.SSVShare
	mu     sync.RWMutex
}

func NewSharesStorage(logger *zap.Logger, db basedb.Database, prefix []byte) (Shares, error) {
	storage := &sharesStorage{
		logger: logger,
		shares: make(map[string]*types.SSVShare),
		db:     db,
		prefix: prefix,
	}
	err := storage.load()
	if err != nil {
		return nil, err
	}
	return storage, nil
}

// load reads all shares from db.
func (s *sharesStorage) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.GetAll(append(s.prefix, sharesPrefix...), func(i int, obj basedb.Obj) error {
		val := &types.SSVShare{}
		if err := val.Decode(obj.Value); err != nil {
			return fmt.Errorf("failed to deserialize share: %w", err)
		}
		s.shares[hex.EncodeToString(val.ValidatorPubKey)] = val
		return nil
	})
}

func (s *sharesStorage) Get(_ basedb.Reader, pubKey []byte) *types.SSVShare {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.shares[hex.EncodeToString(pubKey)]
}

func (s *sharesStorage) List(_ basedb.Reader, filters ...SharesFilter) []*types.SSVShare {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(filters) == 0 {
		return maps.Values(s.shares)
	}

	var shares []*types.SSVShare
Shares:
	for _, share := range s.shares {
		for _, filter := range filters {
			if !filter(share) {
				continue Shares
			}
		}
		shares = append(shares, share)
	}
	return shares
}

func (s *sharesStorage) Save(rw basedb.ReadWriter, shares ...*types.SSVShare) error {
	if len(shares) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.Using(rw).SetMany(s.prefix, len(shares), func(i int) (basedb.Obj, error) {
		value, err := shares[i].Encode()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("failed to serialize share: %w", err)
		}
		return basedb.Obj{Key: s.storageKey(shares[i].ValidatorPubKey), Value: value}, nil
	})
	if err != nil {
		return err
	}

	for _, share := range shares {
		key := hex.EncodeToString(share.ValidatorPubKey)
		s.shares[key] = share
	}
	return nil
}

func (s *sharesStorage) Delete(rw basedb.ReadWriter, pubKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.Using(rw).Delete(s.prefix, s.storageKey(pubKey))
	if err != nil {
		return err
	}

	delete(s.shares, hex.EncodeToString(pubKey))
	return nil
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *sharesStorage) UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	key, err := hex.DecodeString(pk)
	if err != nil {
		return err
	}

	share := s.Get(nil, key)
	if share == nil {
		return nil
	}

	share.BeaconMetadata = metadata
	return s.Save(nil, share)
}

// Drop deletes all shares.
func (s *sharesStorage) Drop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.DropPrefix(bytes.Join(
		[][]byte{s.prefix, sharesPrefix, []byte("/")},
		nil,
	))
	if err != nil {
		return err
	}

	s.shares = make(map[string]*types.SSVShare)
	return nil
}

// storageKey builds share key using sharesPrefix & validator public key, e.g. "shares/0x00..01"
func (s *sharesStorage) storageKey(pk []byte) []byte {
	return bytes.Join([][]byte{sharesPrefix, pk}, []byte("/"))
}

// ByOperatorID filters by operator ID.
func ByOperatorID(operatorID spectypes.OperatorID) SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.BelongsToOperator(operatorID)
	}
}

// ByNotLiquidated filters for not liquidated.
func ByNotLiquidated() SharesFilter {
	return func(share *types.SSVShare) bool {
		return !share.Liquidated
	}
}

// ByActiveValidator filters for active validators.
func ByActiveValidator() SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.HasBeaconMetadata()
	}
}

// ByAttesting filters for attesting validators.
func ByAttesting() SharesFilter {
	return func(share *types.SSVShare) bool {
		return share.HasBeaconMetadata() && share.BeaconMetadata.IsAttesting()
	}
}

// ByClusterID filters by cluster id.
func ByClusterID(clusterID []byte) SharesFilter {
	return func(share *types.SSVShare) bool {
		var operatorIDs []uint64
		for _, op := range share.Committee {
			operatorIDs = append(operatorIDs, op.OperatorID)
		}

		shareClusterID, _ := types.ComputeClusterIDHash(share.OwnerAddress.Bytes(), operatorIDs)
		return bytes.Equal(shareClusterID, clusterID)
	}
}
