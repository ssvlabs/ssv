package validator

import (
	"encoding/hex"
	"strings"
	"sync"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage/basedb"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ICollection interface for validator storage
type ICollection interface {
	eth1.RegistryStore

	SaveValidatorShare(share *beaconprotocol.Share) error
	GetValidatorShare(key []byte) (*beaconprotocol.Share, bool, error)
	GetAllValidatorShares() ([]*beaconprotocol.Share, error)
	GetEnabledOperatorValidatorShares(operatorPubKey string) ([]*beaconprotocol.Share, error)
	GetValidatorSharesByOwnerAddress(ownerAddress string) ([]*beaconprotocol.Share, error)
	DeleteValidatorShare(key []byte) error
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
func NewCollection(options CollectionOptions) validator.ICollection {
	collection := Collection{
		db:     options.DB,
		logger: options.Logger,
		lock:   sync.RWMutex{},
	}
	return &collection
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(share *beaconprotocol.Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	err := s.saveUnsafe(share)
	if err != nil {
		return err
	}
	return nil
}

// SaveValidatorShare save validator share to db
func (s *Collection) saveUnsafe(share *beaconprotocol.Share) error {
	value, err := share.Serialize()
	if err != nil {
		s.logger.Error("failed serialized validator", zap.Error(err))
		return err
	}
	return s.db.Set(collectionPrefix(), share.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*beaconprotocol.Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getUnsafe(key)
}

// GetValidatorShare by key
func (s *Collection) getUnsafe(key []byte) (*beaconprotocol.Share, bool, error) {
	obj, found, err := s.db.Get(collectionPrefix(), key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	share, err := (&beaconprotocol.Share{}).Deserialize(obj.Key, obj.Value)
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
func (s *Collection) GetAllValidatorShares() ([]*beaconprotocol.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*beaconprotocol.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&beaconprotocol.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		res = append(res, val)
		return nil
	})

	return res, err
}

// GetEnabledOperatorValidatorShares returns all not liquidated validator shares belongs to operator
func (s *Collection) GetEnabledOperatorValidatorShares(operatorPubKey string) ([]*beaconprotocol.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*beaconprotocol.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&beaconprotocol.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		if !val.Liquidated {
			if ok := val.IsOperatorShare(operatorPubKey); ok {
				res = append(res, val)
			}
		}
		return nil
	})

	return res, err
}

// GetValidatorSharesByOwnerAddress returns all validator shares belongs to owner address
func (s *Collection) GetValidatorSharesByOwnerAddress(ownerAddress string) ([]*beaconprotocol.Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var res []*beaconprotocol.Share

	err := s.db.GetAll(collectionPrefix(), func(i int, obj basedb.Obj) error {
		val, err := (&beaconprotocol.Share{}).Deserialize(obj.Key, obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to deserialize validator")
		}
		if strings.EqualFold(val.OwnerAddress, ownerAddress) {
			res = append(res, val)
		}
		return nil
	})

	return res, err
}

// DeleteValidatorShare removes validator share by key
func (s *Collection) DeleteValidatorShare(key []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(collectionPrefix(), key)
}

// UpdateValidatorMetadata updates the metadata of the given validator
func (s *Collection) UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error {
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
