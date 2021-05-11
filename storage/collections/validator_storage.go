package collections

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// IValidatorStorage interface for validator storage
type IValidatorStorage interface {
	LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error
	SaveValidatorShare(validator *ValidatorShare) error
	GetAllValidatorsShare() ([]*ValidatorShare, error)
}

// ValidatorShare model for ValidatorStorage struct creation
type ValidatorShare struct {
	NodeID      uint64
	ValidatorPK *bls.PublicKey
	ShareKey    *bls.SecretKey
	Committee   map[uint64]*proto.Node
}

// ValidatorStorage struct
type ValidatorStorage struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

// NewValidatorStorage creates new validator storage
func NewValidatorStorage(db storage.Db, logger *zap.Logger) ValidatorStorage {
	validator := ValidatorStorage{
		prefix: []byte("validator-"),
		db:     db,
		logger: logger,
	}
	return validator
}

// LoadFromConfig fetch validator share form .env and save it to db
func (v *ValidatorStorage) LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error {
	var err error
	if pubKey != (&bls.PublicKey{}) && shareKey != (&bls.SecretKey{}) && len(ibftCommittee) > 0 {
		ibftCommittee[nodeID].Pk = shareKey.GetPublicKey().Serialize()
		ibftCommittee[nodeID].Sk = shareKey.Serialize()
		validator := ValidatorShare{
			NodeID:      nodeID,
			ValidatorPK: pubKey,
			ShareKey:    shareKey,
			Committee:   ibftCommittee,
		}
		err = v.SaveValidatorShare(&validator)
	}
	if err == nil {
		v.logger.Info("validator share has been loaded from config")
	}
	return err
}

// SaveValidatorShare save validator share to db
func (v *ValidatorStorage) SaveValidatorShare(validator *ValidatorShare) error {
	value, err := validator.Serialize()
	if err != nil {
		v.logger.Error("failed serialized validator", zap.Error(err))
	}
	return v.db.Set(v.prefix, validator.ValidatorPK.Serialize(), value)
}

// GetValidatorsShare by key
func (v *ValidatorStorage) GetValidatorsShare(key []byte) (*ValidatorShare, error) {
	obj, err := v.db.Get(v.prefix, key)
	if err != nil{
		return nil, err
	}
	return (&ValidatorShare{}).Deserialize(obj)
}

// GetAllValidatorsShare returns ALL validators shares from db
func (v *ValidatorStorage) GetAllValidatorsShare() ([]*ValidatorShare, error) {
	objs, err := v.db.GetAllByBucket(v.prefix)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get val share")
	}
	var res []*ValidatorShare
	for _, obj := range objs {
		val, err := (&ValidatorShare{}).Deserialize(obj)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to deserialized validator")
		}
		res = append(res, val)
	}

	return res, nil
}

//  serializedValidator struct
type serializedValidator struct {
	NodeID     uint64
	ShareKey   []byte
	Committiee map[uint64]*proto.Node
}

// Serialize ValidatorStorage to []byte for db purposes
func (v *ValidatorShare) Serialize() ([]byte, error) {
	value := serializedValidator{
		NodeID:     v.NodeID,
		ShareKey:   v.ShareKey.Serialize(),
		Committiee: v.Committee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode serializedValidator")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to ValidatorStorage struct
func (v *ValidatorShare) Deserialize(obj storage.Obj) (*ValidatorShare, error) {
	var valShare serializedValidator
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&valShare); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	if err := shareSecret.Deserialize(valShare.ShareKey); err != nil {
		return nil, errors.Wrap(err, "Failed to get key secret")
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(obj.Key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &ValidatorShare{
		NodeID:      valShare.NodeID,
		ValidatorPK: pubKey,
		ShareKey:    shareSecret,
		Committee:   valShare.Committiee,
	}, nil
}
