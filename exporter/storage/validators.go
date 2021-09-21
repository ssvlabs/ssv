package storage

import (
	"bytes"
	"encoding/json"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	validatorsPrefix = []byte("validators")
)

// ValidatorInformation represents a validator
type ValidatorInformation struct {
	Index       int64              `json:"index"`
	PublicKey   string             `json:"publicKey"`
	Balance     spec.Gwei          `json:"balance"`
	Status      v1.ValidatorState  `json:"status"`
	BeaconIndex *uint64            `json:"beacon_index"` // pointer in order to support nil
	Operators   []OperatorNodeLink `json:"operators"`
}

// ValidatorsCollection is the interface for managing validators information
type ValidatorsCollection interface {
	GetValidatorInformation(validatorPubKey string) (*ValidatorInformation, bool, error)
	SaveValidatorInformation(validatorInformation *ValidatorInformation) error
	UpdateValidatorInformation(validatorInformation *ValidatorInformation) error
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

	objs, err := es.db.GetAllByCollection(append(storagePrefix, validatorsPrefix...))
	if err != nil {
		return nil, err
	}
	to = normalTo(to)
	var validators []ValidatorInformation
	for _, obj := range objs {
		var vi ValidatorInformation
		err = json.Unmarshal(obj.Value, &vi)
		if vi.Index >= from && vi.Index <= to {
			validators = append(validators, vi)
		}
	}
	return validators, err
}

// GetValidatorInformation returns information of the given validator by public key
func (es *exporterStorage) GetValidatorInformation(validatorPubKey string) (*ValidatorInformation, bool, error) {
	es.validatorsLock.RLock()
	defer es.validatorsLock.RUnlock()

	obj, found, err := es.db.Get(storagePrefix, validatorKey(validatorPubKey))
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
	info, found, err := es.GetValidatorInformation(validatorInformation.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not read information from DB")
	}

	// lock for writing
	es.validatorsLock.Lock()
	defer es.validatorsLock.Unlock()

	if found {
		es.logger.Debug("validator already exist",
			zap.String("pubKey", validatorInformation.PublicKey))
		validatorInformation.Index = info.Index
		// TODO: update validator information (i.e. change operator)
		return nil
	}
	validatorInformation.Index, err = es.nextIndex(validatorsPrefix)
	if err != nil {
		return errors.Wrap(err, "could not calculate next validator index")
	}
	return es.saveValidatorNotSafe(validatorInformation)
}

func (es *exporterStorage) UpdateValidatorInformation(updatedInfo *ValidatorInformation) error {
	// find validator
	info, found, err := es.GetValidatorInformation(updatedInfo.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not read information from DB")
	}
	if !found {
		return errors.New("validator not found")
	}

	// lock for writing
	es.validatorsLock.Lock()
	defer es.validatorsLock.Unlock()

	// update info
	info.Status = updatedInfo.Status
	info.Balance = updatedInfo.Balance
	info.BeaconIndex = updatedInfo.BeaconIndex
	info.Operators = updatedInfo.Operators

	// save
	return es.saveValidatorNotSafe(info)
}

func (es *exporterStorage) saveValidatorNotSafe(val *ValidatorInformation) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return errors.Wrap(err, "could not marshal validator information")
	}
	return es.db.Set(storagePrefix, validatorKey(val.PublicKey), raw)
}

func validatorKey(pubKey string) []byte {
	return bytes.Join([][]byte{
		validatorsPrefix[:],
		[]byte(pubKey),
	}, []byte("/"))
}
