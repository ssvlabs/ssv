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

// IValidator interface for validator storage
type IValidator interface {
	LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error
	SaveValidatorShare(validator *Validator) error
	GetAllValidatorsShare() ([]*Validator, error)
}

// Validator model for ValidatorStorage struct creation
type Validator struct {
	NodeID     uint64
	PubKey     *bls.PublicKey
	ShareKey   *bls.SecretKey
	Committiee map[uint64]*proto.Node
}

// ValidatorStorage struct
type ValidatorStorage struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

// NewValidator creates new validator storage
func NewValidator(db storage.Db, logger *zap.Logger) ValidatorStorage {
	validator := ValidatorStorage{
		prefix: []byte("validator"),
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
		validator := Validator{
			NodeID:     nodeID,
			PubKey:     pubKey,
			ShareKey:   shareKey,
			Committiee: ibftCommittee,
		}
		err = v.SaveValidatorShare(&validator)
	}
	if err == nil {
		v.logger.Info("validator share has been loaded from config")
	}
	return err
}

// SaveValidatorShare save validator share to db
func (v *ValidatorStorage) SaveValidatorShare(validator *Validator) error {
	value, err := validator.Serialize()
	if err != nil {
		v.logger.Error("failed serialized validator", zap.Error(err))
	}
	return v.db.Set(v.prefix, validator.PubKey.Serialize(), value)
}

// GetAllValidatorsShare returns ALL validators shares from db
func (v *ValidatorStorage) GetAllValidatorsShare() ([]*Validator, error) {
	objs, err := v.db.GetAllByBucket(v.prefix)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get val share")
	}
	var res []*Validator
	for _, obj := range objs {
		val, err := (&Validator{}).Deserialize(obj)
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
func (v *Validator) Serialize() ([]byte, error) {
	value := serializedValidator{
		NodeID:     v.NodeID,
		ShareKey:   v.ShareKey.Serialize(),
		Committiee: v.Committiee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode serializedValidator")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to ValidatorStorage struct
func (v *Validator) Deserialize(obj storage.Obj) (*Validator, error) {
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
	return &Validator{
		NodeID:     valShare.NodeID,
		PubKey:     pubKey,
		ShareKey:   shareSecret,
		Committiee: valShare.Committiee,
	}, nil
}
