package storage

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ICollection interface for validator storage
type ICollection interface {
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

// ShareOptions - used to load validator share from config
type ShareOptions struct {
	NodeID    uint64         `yaml:"NodeID" env:"NodeID" env-description:"Local share node ID"`
	PublicKey string         `yaml:"PublicKey" env:"LOCAL_NODE_ID" env-description:"Local validator public key"`
	ShareKey  string         `yaml:"ShareKey" env:"LOCAL_SHARE_KEY" env-description:"Local share key"`
	Committee map[string]int `yaml:"Committee" env:"LOCAL_COMMITTEE" env-description:"Local validator committee array"`
}

// ToShare creates a Share instance from ShareOptions
func (options *ShareOptions) ToShare() (*Share, error) {
	var err error

	if len(options.PublicKey) > 0 && len(options.ShareKey) > 0 && len(options.Committee) > 0 {
		shareKey := &bls.SecretKey{}

		if err = shareKey.SetHexString(options.ShareKey); err != nil {
			return nil, errors.Wrap(err, "failed to set hex private key")
		}
		validatorPk := &bls.PublicKey{}
		if err = validatorPk.DeserializeHexStr(options.PublicKey); err != nil {
			return nil, errors.Wrap(err, "failed to decode validator key")
		}

		_getBytesFromHex := func(str string) []byte {
			val, e := hex.DecodeString(str)
			if e != nil {
				err = errors.Wrap(err, "failed to decode committee")
			}
			return val
		}
		ibftCommittee := make(map[uint64]*proto.Node)

		for pk, id := range options.Committee {
			ibftCommittee[uint64(id)] = &proto.Node{
				IbftId: uint64(id),
				Pk:     _getBytesFromHex(pk),
			}
			if uint64(id) == options.NodeID {
				ibftCommittee[options.NodeID].Pk = shareKey.GetPublicKey().Serialize()
				ibftCommittee[options.NodeID].Sk = shareKey.Serialize()
			}
		}

		if err != nil {
			return nil, err
		}

		share := Share{
			NodeID:    options.NodeID,
			PublicKey: validatorPk,
			ShareKey:  shareKey,
			Committee: ibftCommittee,
		}
		return &share, nil
	}
	return nil, errors.New("empty share")
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

// LoadFromConfig fetch validator share form .env and save it to db
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
