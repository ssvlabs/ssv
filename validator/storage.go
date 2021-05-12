package validator

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// IValidator interface for validator storage
type ICollection interface {
	LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error
	SaveValidatorShare(share *Share) error
	GetAllValidatorsShare() ([]*Share, error)
}

// Collection struct
type collectionOptions struct {
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
		db: *options.DB,
		logger: options.Logger,
		prefix: []byte(GetCollectionPrefix()),
	}
	return &collection
}
func GetCollectionPrefix() string {
	return "share-"
}

// LoadFromConfig fetch validator share form .env and save it to db
func (s *Collection) LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error {
	var err error
	if pubKey != (&bls.PublicKey{}) && shareKey != (&bls.SecretKey{}) && len(ibftCommittee) > 0 {
		ibftCommittee[nodeID].Pk = shareKey.GetPublicKey().Serialize()
		ibftCommittee[nodeID].Sk = shareKey.Serialize()
		validator := Share{
			NodeID:    nodeID,
			PublicKey: pubKey,
			ShareKey:  shareKey,
			Committee: ibftCommittee,
		}
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
	objs, err := s.db.GetAllByBucket(s.prefix)
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
