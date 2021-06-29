package storage

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	operatorsPrefix = []byte("operators")
)

// OperatorInformation the public data of an operator
type OperatorInformation struct {
	PublicKey    []byte         `json:"publicKey"`
	Name         string         `json:"name"`
	OwnerAddress common.Address `json:"ownerAddress"`
	Index        int64          `json:"index"`
}

// OperatorsCollection is the interface for managing operators information
type OperatorsCollection interface {
	GetOperatorInformation(operatorPubKey []byte) (*OperatorInformation, error)
	SaveOperatorInformation(operatorInformation *OperatorInformation) error
	ListOperators(from int64, to int64) ([]OperatorInformation, error)
}

// ListOperators returns information of all the known operators
// when 'to' equals zero, all operators will be returned
func (es *exporterStorage) ListOperators(from int64, to int64) ([]OperatorInformation, error) {
	objs, err := es.db.GetAllByCollection(append(storagePrefix, operatorsPrefix...))
	if err != nil {
		return nil, err
	}
	to = normalTo(to)
	var operators []OperatorInformation
	for _, obj := range objs {
		var oi OperatorInformation
		err = json.Unmarshal(obj.Value, &oi)
		if oi.Index >= from && oi.Index <= to {
			operators = append(operators, oi)
		}
	}
	return operators, err
}

// GetOperatorInformation returns information of the given operator by public key
func (es *exporterStorage) GetOperatorInformation(operatorPubKey []byte) (*OperatorInformation, error) {
	obj, err := es.db.Get(storagePrefix, operatorKey(operatorPubKey))
	if err != nil {
		return nil, err
	}
	var operatorInformation OperatorInformation
	err = json.Unmarshal(obj.Value, &operatorInformation)
	return &operatorInformation, err
}

// SaveOperatorInformation saves operator information by its public key
func (es *exporterStorage) SaveOperatorInformation(operatorInformation *OperatorInformation) error {
	existing, err := es.GetOperatorInformation(operatorInformation.PublicKey)
	if err != nil && err.Error() != kv.EntryNotFoundError {
		return errors.Wrap(err, "could not read information from DB")
	}
	if existing != nil {
		es.logger.Debug("operator already exist",
			zap.String("pubKey", hex.EncodeToString(operatorInformation.PublicKey)))
		operatorInformation.Index = existing.Index
		// TODO: update operator information (i.e. change name)
		return nil
	}
	operatorInformation.Index, err = es.nextIndex(operatorsPrefix)
	if err != nil {
		return errors.Wrap(err, "could not calculate next operator index")
	}
	raw, err := json.Marshal(operatorInformation)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
	}
	return es.db.Set(storagePrefix, operatorKey(operatorInformation.PublicKey), raw)
}

func operatorKey(pubKey []byte) []byte {
	return bytes.Join([][]byte{
		operatorsPrefix[:],
		pubKey[:],
	}, []byte("/"))
}
