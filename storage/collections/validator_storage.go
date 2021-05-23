package collections

import (
	"bytes"
	"encoding/gob"
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

// IValidatorStorage interface for validator storage
type IValidatorStorage interface {
	GetDBEvent() *storage.DBEvent
	LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error
	SaveValidatorShare(validator *ValidatorShare) error
	GetAllValidatorShares() ([]*ValidatorShare, error)
	// InformObserver informs observer
	InformObserver(i interface{})
	// GetObserverID get the observer id
	GetObserverID() string
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
	prefix  []byte
	db      storage.IKvStorage
	logger  *zap.Logger
	dbEvent *storage.DBEvent
	pubsub.BaseObserver
}

// InformObserver informs observer
func (v *ValidatorStorage) InformObserver(data interface{}) {
	if validatorAddedEvent, ok := data.(eth1.ValidatorAddedEvent); ok {
		validatorShare := ValidatorShare{}
		ibftCommittee := map[uint64]*proto.Node{}
		for i := range validatorAddedEvent.OessList {
			oess := validatorAddedEvent.OessList[i]
			nodeID := oess.Index.Uint64() + 1
			ibftCommittee[nodeID] = &proto.Node{
				IbftId: nodeID,
				Pk:     oess.SharedPublicKey,
			}
			if strings.EqualFold(string(oess.OperatorPublicKey), params.SsvConfig().OperatorPublicKey) {
				validatorShare.NodeID = nodeID

				validatorShare.ValidatorPK = &bls.PublicKey{}
				if err := validatorShare.ValidatorPK.Deserialize(validatorAddedEvent.PublicKey); err != nil {
					v.logger.Error("failed to deserialize share public key", zap.Error(err))
					return
				}

				validatorShare.ShareKey = &bls.SecretKey{}
				if err := validatorShare.ShareKey.SetHexString(string(oess.EncryptedKey)); err != nil {
					v.logger.Error("failed to deserialize share private key", zap.Error(err))
					return
				}
				ibftCommittee[nodeID].Sk = validatorShare.ShareKey.Serialize()
			}
		}
		validatorShare.Committee = ibftCommittee
		if err := v.SaveValidatorShare(&validatorShare); err != nil {
			v.logger.Error("failed to save validator share", zap.Error(err))
			return
		}

		v.dbEvent.Data = validatorShare
		v.dbEvent.NotifyAll()
	}
}

// GetObserverID get the observer id
func (v *ValidatorStorage) GetObserverID() string {
	// TODO return proper id for the observer
	return "ValidatorStorageObserver"
}

// NewValidatorStorage creates new validator storage
func NewValidatorStorage(db storage.IKvStorage, logger *zap.Logger) ValidatorStorage {
	validator := ValidatorStorage{
		prefix:  []byte("validator-"),
		db:      db,
		logger:  logger,
		dbEvent: storage.NewDBEvent("dbEvent"),
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
	if err != nil {
		return nil, err
	}
	return (&ValidatorShare{}).Deserialize(obj)
}

// GetAllValidatorShares returns ALL validator shares from db
func (v *ValidatorStorage) GetAllValidatorShares() ([]*ValidatorShare, error) {
	objs, err := v.db.GetAllByCollection(v.prefix)
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

//  validatorSerializer struct
type validatorSerializer struct {
	NodeID     uint64
	ShareKey   []byte
	Committiee map[uint64]*proto.Node
}

// Serialize ValidatorStorage to []byte for db purposes
func (v *ValidatorShare) Serialize() ([]byte, error) {
	value := validatorSerializer{
		NodeID:     v.NodeID,
		ShareKey:   v.ShareKey.Serialize(),
		Committiee: v.Committee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode validatorSerializer")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to ValidatorStorage struct
func (v *ValidatorShare) Deserialize(obj storage.Obj) (*ValidatorShare, error) {
	var valShare validatorSerializer
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

// GetDBEvent returns the dbEvent
func (v *ValidatorStorage) GetDBEvent() *storage.DBEvent {
	return v.dbEvent
}


