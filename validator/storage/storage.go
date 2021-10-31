package storage

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// ICollection interface for validator storage
type ICollection interface {
	SaveValidatorShare(share *Share) error
	GetValidatorShare(key []byte) (*Share, bool, error)
	GetAllValidatorsShare() ([]*Share, error)
	CleanAllShares() error
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
	prefix []byte
}

// NewCollection creates new share storage
func NewCollection(options CollectionOptions) ICollection {
	collection := Collection{
		db:     options.DB,
		logger: options.Logger,
		prefix: []byte(getCollectionPrefix()),
		lock:   sync.RWMutex{},
	}
	return &collection
}
func getCollectionPrefix() string {
	return "share-"
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(validator *Share) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	value, err := validator.Serialize()
	if err != nil {
		s.logger.Error("failed serialized validator", zap.Error(err))
		return err
	}
	return s.db.Set(s.prefix, validator.PublicKey.Serialize(), value)
}

// GetValidatorShare by key
func (s *Collection) GetValidatorShare(key []byte) (*Share, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	obj, found, err := s.db.Get(s.prefix, key)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	share, err := (&Share{}).Deserialize(obj)
	return share, found, err
}

// CleanAllShares cleans all existing shares from DB
func (s *Collection) CleanAllShares() error {
	return s.db.RemoveAllByCollection(s.prefix)
}

// GetAllValidatorsShare returns all shares
func (s *Collection) GetAllValidatorsShare() ([]*Share, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	objs, err := s.db.GetAllByCollection(s.prefix)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get val share")
	}
	var res []*Share
	for _, obj := range objs {
		val, err := (&Share{}).Deserialize(obj)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to deserialize validator")
		}
		res = append(res, val)
	}

	return res, nil
}
