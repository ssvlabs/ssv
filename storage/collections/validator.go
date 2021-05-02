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

// ValidatorOptions for Validator struct creation
type ValidatorOptions struct {
	ValidatorPk *bls.PublicKey
	ShareKey    *bls.SecretKey
	Committiee  map[uint64]*proto.Node
}

// Validator struct
type Validator struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

//  validatorShare struct
type validatorShare struct {
	ShareKey   []byte
	Committiee map[uint64]*proto.Node
}

// NewValidator creates new validator storage
func NewValidator(db storage.Db, logger *zap.Logger, opts ValidatorOptions) Validator {
	validator := Validator{
		prefix: []byte("validator"),
		db:     db,
		logger: logger,
	}

	validator.LoadFromConfig(opts.ValidatorPk, opts.ShareKey, opts.Committiee)
	return validator
}

// LoadFromConfig fetch validator share form .env and save it to db
func (v *Validator) LoadFromConfig(validatorPubKey *bls.PublicKey, sharePrivateKey *bls.SecretKey, committee map[uint64]*proto.Node) {
	err := v.SaveValidatorShare(validatorPubKey, sharePrivateKey, committee)
	if err != nil {
		v.logger.Error("Failed to load validator share data from config", zap.Error(err))
	}
}

// SaveValidatorShare save validator share to db
func (v *Validator) SaveValidatorShare(pubkey *bls.PublicKey, shareKey *bls.SecretKey, committiee map[uint64]*proto.Node) error {
	value := validatorShare{
		ShareKey:   shareKey.Serialize(),
		Committiee: committiee,
	}
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		v.logger.Error("Failed to encode validatorShare", zap.Error(err))
	}
	return v.db.Set(v.prefix, pubkey.Serialize(), b.Bytes())
}

// GetValidatorShare returns ALL validators shares from db
func (v *Validator) GetValidatorShare() (*bls.PublicKey, *bls.SecretKey, map[uint64]*proto.Node, error) { // TODO: temp getting the :0 from slice. need to support multi valShare
	res, err := v.db.GetAllByBucket(v.prefix)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to get val share")
	}

	var valShare validatorShare
	d := gob.NewDecoder(bytes.NewReader(res[0].Value))
	if err := d.Decode(&valShare); err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	err = shareSecret.Deserialize(valShare.ShareKey)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to get key secret")
	}

	pubKey := &bls.PublicKey{}
	err = pubKey.Deserialize(res[0].Key)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return pubKey, shareSecret, valShare.Committiee, err
}
