package storage

import (
	"bytes"
	"encoding/json"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func validatorsPrefix() []byte {
	return []byte("validators")
}

// ValidatorInformation represents a validator
type ValidatorInformation struct {
	Index     int64              `json:"index"`
	PublicKey string             `json:"publicKey"`
	Operators []OperatorNodeLink `json:"operators"`
}

// ValidatorsCollection is the interface for managing validators information
type ValidatorsCollection interface {
	GetValidatorInformation(validatorPubKey string) (*ValidatorInformation, bool, error)
	SaveValidatorInformation(validatorInformation *ValidatorInformation) error
	ListValidators(from int64, to int64) ([]ValidatorInformation, error)
}

// OperatorNodeLink links a validator to an operator
type OperatorNodeLink struct {
	ID        uint64 `json:"nodeId"`
	PublicKey string `json:"publicKey"`
}

// ListValidators returns information of all the known validators
// when 'to' equals zero, all validators will be returned
func (es *exporterStorage) ListValidators(from int64, to int64) ([]ValidatorInformation, error) {
	es.validatorsLock.RLock()
	defer es.validatorsLock.RUnlock()

	to = normalTo(to)
	var validators []ValidatorInformation
	err := es.db.GetAll(append(storagePrefix(), validatorsPrefix()...), func(i int, obj basedb.Obj) error {
		var vi ValidatorInformation
		if err := json.Unmarshal(obj.Value, &vi); err != nil {
			return err
		}
		if vi.Index >= from && vi.Index <= to {
			validators = append(validators, vi)
		}
		return nil
	})
	return validators, err
}

// GetValidatorInformation returns information of the given validator by public key
func (es *exporterStorage) GetValidatorInformation(validatorPubKey string) (*ValidatorInformation, bool, error) {
	es.validatorsLock.RLock()
	defer es.validatorsLock.RUnlock()

	return es.getValidatorInformationNotSafe(validatorPubKey)
}

// GetValidatorInformation returns information of the given validator by public key
func (es *exporterStorage) getValidatorInformationNotSafe(validatorPubKey string) (*ValidatorInformation, bool, error) {
	obj, found, err := es.db.Get(storagePrefix(), validatorKey(validatorPubKey))
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	var vi ValidatorInformation
	err = json.Unmarshal(obj.Value, &vi)
	return &vi, found, err
}

// SaveValidatorInformation saves validator information by its public key
func (es *exporterStorage) SaveValidatorInformation(validatorInformation *ValidatorInformation) error {
	es.validatorsLock.Lock()
	defer es.validatorsLock.Unlock()

	info, found, err := es.getValidatorInformationNotSafe(validatorInformation.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not read information from DB")
	}

	if found {
		es.logger.Debug("validator information exist",
			zap.String("pubKey", validatorInformation.PublicKey))
		validatorInformation.Index = info.Index
		// TODO: update validator information (i.e. change operator)
		return nil
	}
	validatorInformation.Index, err = es.nextIndex(validatorsPrefix())
	if err != nil {
		return errors.Wrap(err, "could not calculate next validator index")
	}
	return es.saveValidatorNotSafe(validatorInformation)
}

func (es *exporterStorage) saveValidatorNotSafe(val *ValidatorInformation) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return errors.Wrap(err, "could not marshal validator information")
	}
	return es.db.Set(storagePrefix(), validatorKey(val.PublicKey), raw)
}

func validatorKey(pubKey string) []byte {
	return bytes.Join([][]byte{
		validatorsPrefix(),
		[]byte(pubKey),
	}, []byte("/"))
}
