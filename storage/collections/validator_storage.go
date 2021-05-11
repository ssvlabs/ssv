package collections

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"strings"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/storage"
)

// IValidator interface for validator storage
type IValidator interface {
	GetDBEvent() *storage.DBEvent
	LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error
	SaveValidatorShare(validator *Validator) error
	GetAllValidatorShares() ([]*Validator, error)
	// Update updates observer
	Update(i interface{})
	// GetID get the observer id
	GetID() string
}

// Validator model for ValidatorStorage struct creation
type Validator struct {
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
	dbEvent *storage.DBEvent
	pubsub.BaseObserver
}

// Update updates observer
func (v *ValidatorStorage) Update(data interface{}) {

	v.logger.Info("Got log from contract", zap.Any("log", data))

	if validatorEvent, ok := data.(eth1.ValidatorEvent); ok {
		validator := Validator{}
		ibftCommittee := map[uint64]*proto.Node{}
		oessList := validatorEvent.OessList
		for i := range oessList {
			nodeID := oessList[i].Index.Uint64() + 1
			ibftCommittee[nodeID] = &proto.Node{
				IbftId: nodeID,
				Pk:     oessList[i].SharePubKey,
			}
			if strings.EqualFold(hex.EncodeToString(oessList[i].OperatorPubKey), params.SsvConfig().OperatorPublicKey) {
				validator.NodeID = nodeID

				validator.ValidatorPK = &bls.PublicKey{}
				if err := validator.ValidatorPK.Deserialize(validatorEvent.ValidatorPubKey); err != nil {
					v.logger.Error("failed to deserialize share public key", zap.Error(err))
					return
				}

				// TODO: decrypt share private key using operator private key
				validator.ShareKey = &bls.SecretKey{}
				if err := validator.ShareKey.Deserialize(oessList[i].EncryptedKey); err != nil {
					v.logger.Error("failed to deserialize share private key", zap.Error(err))
					return
				}
				ibftCommittee[nodeID].Sk = oessList[i].EncryptedKey

			}
		}
		validator.Committee = ibftCommittee
		if err := v.SaveValidatorShare(&validator); err != nil {
			v.logger.Error("failed to save validator share", zap.Error(err))
			return
		}

		v.dbEvent.Data = validator
		v.dbEvent.NotifyAll()
	}
}

// GetID get the observer id
func (v *ValidatorStorage) GetID() string {
	// TODO return proper id for the observer
	return "ValidatorStorageObserver"
}

// NewValidatorStorage creates new validator storage
func NewValidatorStorage(db storage.Db, logger *zap.Logger) ValidatorStorage {
	validator := ValidatorStorage{
		prefix: []byte("validator-"),
		db:     db,
		logger: logger,
		dbEvent: storage.NewDBEvent("dbtEvent"),
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
func (v *ValidatorStorage) SaveValidatorShare(validator *Validator) error {
	value, err := validator.Serialize()
	if err != nil {
		v.logger.Error("failed serialized validator", zap.Error(err))
	}
	return v.db.Set(v.prefix, validator.ValidatorPK.Serialize(), value)
}

// GetValidatorsShare by key
func (v *ValidatorStorage) GetValidatorsShare(key []byte) (*Validator, error) {
	obj, err := v.db.Get(v.prefix, key)
	if err != nil{
		return nil, err
	}
	return (&Validator{}).Deserialize(obj)
}

// GetAllValidatorShares returns ALL validators shares from db
func (v *ValidatorStorage) GetAllValidatorShares() ([]*Validator, error) {
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
		NodeID:      valShare.NodeID,
		ValidatorPK: pubKey,
		ShareKey:    shareSecret,
		Committee:   valShare.Committiee,
	}, nil
}

// GetDBEvent returns the dbEvent
func (v *ValidatorStorage) GetDBEvent() *storage.DBEvent {
	return v.dbEvent
}
