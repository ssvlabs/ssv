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
		shareKey := &bls.SecretKey{}

		if err := shareKey.SetHexString(options.ShareKey); err != nil {
			s.logger.Fatal("failed to set hex private key", zap.Error(err))
		}
		validatorPk := &bls.PublicKey{}
		if err := validatorPk.DeserializeHexStr(options.PublicKey); err != nil {
			s.logger.Fatal("failed to decode validator key", zap.Error(err))
		}

		_getBytesFromHex := func(str string) []byte {
			val, err := hex.DecodeString(str)
			if err != nil {
				s.logger.Fatal("failed to decode committee", zap.Error(err))
			}
			return val
		}
		ibftCommittee := make(map[uint64]*proto.Node)

		for pk, id := range options.Committee {
			// TODO  remove test log
			s.logger.Info("loaded env committee", zap.String("pk", pk), zap.Int("id", id))
			ibftCommittee[uint64(id)] = &proto.Node{
				IbftId: uint64(id),
				Pk:     _getBytesFromHex(pk),
			}
			if uint64(id) == options.NodeID {
				ibftCommittee[options.NodeID].Pk = shareKey.GetPublicKey().Serialize()
				ibftCommittee[options.NodeID].Sk = shareKey.Serialize()
			}
		}

		validator := Share{
			NodeID:    options.NodeID,
			PublicKey: validatorPk,
			ShareKey:  shareKey,
			Committee: ibftCommittee,
		}
		s.logger.Info(validator.ShareKey.SerializeToHexStr())
		err = s.SaveValidatorShare(&validator)
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
