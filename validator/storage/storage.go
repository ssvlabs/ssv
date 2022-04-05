package storage

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/validator/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// ICollection interface for validator storage
type ICollection interface {
	basedb.RegistryStore

	SaveValidatorShare(share *types.Share) error
	GetValidatorShare(key []byte) (*types.Share, bool, error)
	GetAllValidatorShares() ([]*types.Share, error)
	GetOperatorValidatorShares(operatorPubKey string) ([]*types.Share, error)
}

func collectionPrefix() []byte {
	return []byte("share-")
}

// CollectionOptions struct
type CollectionOptions struct {
	DB     basedb.IDb
	Logger *zap.Logger
}

// Collection struct
type Collection struct {
	db     basedb.IDb
	logger *zap.Logger
	lock   sync.RWMutex
}

// NewCollection creates new share storage
func NewCollection(options CollectionOptions) ICollection {
	collection := Collection{
		db:     options.DB,
		logger: options.Logger,
		lock:   sync.RWMutex{},
	}
	return &collection
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(share *types.Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(share)
	if err != nil {
		return err
	}
	return nil
}

// SaveValidatorShare save validator share to db
func (s *Collection) saveUnsafe(share *types.Share) error {
	value, err := share.Serialize()
	if err != nil {
		s.logger.Error("failed serialized validator", zap.Error(err))
		return err
	}
	return s.db.Set(collectionPrefix(), share.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*types.Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(key)
}

// GetValidatorShare by key
func (s *Collection) getUnsafe(key []byte) (*types.Share, bool, error) {
	obj, found, err := s.db.Get(collectionPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	share, err := (&types.Share{}).Deserialize(obj.Key, obj.Value)
	return share, found, err
}

// CleanRegistryData clears all registry data
func (s *Collection) CleanRegistryData() error {
	return s.cleanAllShares()
}

func (s *Collection) cleanAllShares() error {
	return s.db.RemoveAllByCollection(collectionPrefix())
}

// GetAllValidatorShares returns all shares
func (s *Collection) GetAllValidatorShares() ([]*types.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&types.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetOperatorValidatorShares returns all validator shares belongs to operator
func (s *Collection) GetOperatorValidatorShares(operatorPubKey string) ([]*types.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*types.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&types.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		ok := val.IsOperatorShare(operatorPubKey)
		if ok {
			res = append(res, val)
		}
		return nil
	})

	return res, err
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *Collection) UpdateValidatorMetadata(pk string, metadata *beacon.ValidatorMetadata) error {
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
	share.Metadata = metadata
	return s.saveUnsafe(share)
}
