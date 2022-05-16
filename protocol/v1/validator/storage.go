package validator

import (
	"encoding/hex"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(share *beacon.Share) error
	GetValidatorShare(key []byte) (*beacon.Share, bool, error)
	GetAllValidatorShares() ([]*beacon.Share, error)
	GetOperatorValidatorShares(operatorPubKey string) ([]*beacon.Share, error)
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
func (s *Collection) SaveValidatorShare(share *beacon.Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(share)
	if err != nil {
		return err
	}
	return nil
}

// SaveValidatorShare save validator share to db
func (s *Collection) saveUnsafe(share *beacon.Share) error {
	value, err := share.Serialize()
	if err != nil {
		s.logger.Error("failed serialized validator", zap.Error(err))
		return err
	}
	return s.db.Set(collectionPrefix(), share.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*beacon.Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(key)
}

// GetValidatorShare by key
func (s *Collection) getUnsafe(key []byte) (*beacon.Share, bool, error) {
	obj, found, err := s.db.Get(collectionPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	share, err := (&beacon.Share{}).Deserialize(obj.Key, obj.Value)
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
func (s *Collection) GetAllValidatorShares() ([]*beacon.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*beacon.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&beacon.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return fmt.Errorf("failed to deserialize validator: %w", err)
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetOperatorValidatorShares returns all validator shares belongs to operator
func (s *Collection) GetOperatorValidatorShares(operatorPubKey string) ([]*beacon.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*beacon.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&beacon.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return fmt.Errorf("failed to deserialize validator: %w", err)
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
