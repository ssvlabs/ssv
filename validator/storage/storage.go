package storage

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// ICollection interface for validator storage
type ICollection interface {
	LoadMultipleFromConfig(items []ShareOptions)
	LoadFromConfig(options ShareOptions) (string, error)
	SaveValidatorShare(share *Share) error
	GetValidatorsShare(key []byte) (*Share, bool, error)
	GetAllValidatorsShare() ([]*Share, error)
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

// LoadMultipleFromConfig fetch multiple validators share from config and save it to db
func (s *Collection) LoadMultipleFromConfig(items []ShareOptions) {
	var addedValidators []string
	if len(items) > 0 {
		s.logger.Info("loading validators share from config", zap.Int("count", len(items)))
		for _, opts := range items {
			pubkey, err := s.LoadFromConfig(opts)
			if err != nil {
				s.logger.Error("failed to load validator share data from config", zap.Error(err))
				continue
			}
			addedValidators = append(addedValidators, pubkey)
		}
		s.logger.Info("successfully loaded validators from config", zap.Strings("pubkeys", addedValidators))
	}
}

// LoadFromConfig fetch validator share from config and save it to db
func (s *Collection) LoadFromConfig(options ShareOptions) (string, error) {
	if len(options.PublicKey) == 0 || len(options.ShareKey) == 0 || len(options.Committee) == 0 {
		return "", errors.New("one or more fields are missing (PublicKey, ShareKey, Committee)")
	}
	share, err := options.ToShare()
	if err != nil {
		return "", errors.WithMessage(err, "failed to create share object")
	} else if share != nil {
		pubKey := share.ShareKey.SerializeToHexStr()
		err := s.SaveValidatorShare(share)
		return pubKey, err
	}
	return "", errors.New("returned nil share")
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

// GetValidatorsShare by key
func (s *Collection) GetValidatorsShare(key []byte) (*Share, bool, error) {
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

// GetAllValidatorsShare returns ALL validators shares from db
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
