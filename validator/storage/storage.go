package storage

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ICollection interface for validator storage
type ICollection interface {
	LoadMultipleFromConfig(items []ShareOptions) error
	LoadFromConfig(options ShareOptions) error
	SaveValidatorShare(share *Share) error
	GetValidatorsShare(key []byte) (*Share, error)
	GetAllValidatorsShare() ([]*Share, error)
}

// CollectionOptions struct
type CollectionOptions struct {
	DB     *basedb.IDb
	Logger *zap.Logger
}

// Collection struct
type Collection struct {
	db     basedb.IDb
	logger *zap.Logger
	prefix []byte
}

// NewCollection creates new share storage
func NewCollection(options CollectionOptions) ICollection {
	collection := Collection{
		db:     *options.DB,
		logger: options.Logger,
		prefix: []byte(getCollectionPrefix()),
	}
	return &collection
}
func getCollectionPrefix() string {
	return "share-"
}

// LoadMultipleFromConfig fetch multiple validators share from config and save it to db
func (s *Collection) LoadMultipleFromConfig(items []ShareOptions) error {
	if len(items) > 0 {
		for _, opts := range items {
			if err := s.LoadFromConfig(opts); err != nil {
				s.logger.Error("failed to load validator share data from config", zap.Error(err))
				return err
			}
		}
	}
	return nil
}

// LoadFromConfig fetch validator share from config and save it to db
func (s *Collection) LoadFromConfig(options ShareOptions) error {
	var err error
	if len(options.PublicKey) > 0 && len(options.ShareKey) > 0 && len(options.Committee) > 0 {
		share, e := options.ToShare()
		if e != nil {
			err = e
			s.logger.Fatal("failed to create share object:", zap.Error(err))
		} else if share != nil {
			s.logger.Info(share.ShareKey.SerializeToHexStr())
			err = s.SaveValidatorShare(share)
		}
	}
	if err == nil {
		s.logger.Info("validator share has been loaded from config")
	}
	return err
}

// SaveValidatorShare save validator share to db
func (s *Collection) SaveValidatorShare(validator *Share) error {
	value, err := validator.Serialize()
	if err != nil {
		s.logger.Error("failed serialized validator", zap.Error(err))
		return err
	}
	return s.db.Set(s.prefix, validator.PublicKey.Serialize(), value)
}

// GetValidatorsShare by key
func (s *Collection) GetValidatorsShare(key []byte) (*Share, error) {
	obj, err := s.db.Get(s.prefix, key)
	if err != nil {
		return nil, err
	}
	return (&Share{}).Deserialize(obj)
}

// GetAllValidatorsShare returns ALL validators shares from db
func (s *Collection) GetAllValidatorsShare() ([]*Share, error) {
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
