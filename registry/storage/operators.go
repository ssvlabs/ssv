package storage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	operatorsPrefix = []byte("operators")
)

// OperatorData the public data of an operator
type OperatorData struct {
	ID           spectypes.OperatorID `json:"id"`
	PublicKey    []byte               `json:"publicKey"`
	OwnerAddress common.Address       `json:"ownerAddress"`
}

// GetOperatorData is a function that returns the operator data
type GetOperatorData = func(index uint64) (*OperatorData, bool, error)

// Operators is the interface for managing operators data
type Operators interface {
	GetOperatorDataByPubKey(txn basedb.Txn, operatorPubKey []byte) (*OperatorData, bool, error)
	GetOperatorData(txn basedb.Txn, id spectypes.OperatorID) (*OperatorData, bool, error)
	SaveOperatorData(txn basedb.Txn, operatorData *OperatorData) (bool, error)
	DeleteOperatorData(txn basedb.Txn, id spectypes.OperatorID) error
	ListOperators(txn basedb.Txn, from uint64, to uint64) ([]OperatorData, error)
	GetOperatorsPrefix() []byte
}

type operatorsStorage struct {
	logger *zap.Logger
	db     basedb.Database
	lock   sync.RWMutex
	prefix []byte
}

// NewOperatorsStorage creates a new instance of Storage
func NewOperatorsStorage(logger *zap.Logger, db basedb.Database, prefix []byte) Operators {
	return &operatorsStorage{
		logger: logger,
		db:     db,
		prefix: prefix,
	}
}

// GetOperatorsPrefix returns the prefix
func (s *operatorsStorage) GetOperatorsPrefix() []byte {
	return operatorsPrefix
}

// ListOperators returns data of the all known operators by index range (from, to)
// when 'to' equals zero, all operators will be returned
func (s *operatorsStorage) ListOperators(txn basedb.Txn, from uint64, to uint64) ([]OperatorData, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.listOperators(txn, from, to)
}

// GetOperatorData returns data of the given operator by index
func (s *operatorsStorage) GetOperatorData(txn basedb.Txn, id spectypes.OperatorID) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorData(txn, id)
}

// GetOperatorDataByPubKey returns data of the given operator by public key
func (s *operatorsStorage) GetOperatorDataByPubKey(txn basedb.Txn, operatorPubKey []byte) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorDataByPubKey(txn, operatorPubKey)
}

func (s *operatorsStorage) getOperatorDataByPubKey(txn basedb.Txn, operatorPubKey []byte) (*OperatorData, bool, error) {
	operatorsData, err := s.listOperators(txn, 0, 0)
	if err != nil {
		return nil, false, errors.Wrap(err, "could not get all operators")
	}
	for _, op := range operatorsData {
		if bytes.Equal(op.PublicKey, operatorPubKey) {
			return &op, true, nil
		}
	}
	return nil, false, nil
}

func (s *operatorsStorage) getOperatorData(txn basedb.Txn, id spectypes.OperatorID) (*OperatorData, bool, error) {
	getter := s.db.Get
	if txn != nil {
		getter = txn.Get
	}

	obj, found, err := getter(s.prefix, buildOperatorKey(id))
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, found, nil
	}

	var operatorInformation OperatorData
	err = json.Unmarshal(obj.Value, &operatorInformation)
	return &operatorInformation, found, err
}

func (s *operatorsStorage) listOperators(txn basedb.Txn, from, to uint64) ([]OperatorData, error) {
	getter := s.db.GetAll
	if txn != nil {
		getter = txn.GetAll
	}

	var operators []OperatorData
	err := getter(append(s.prefix, operatorsPrefix...), func(i int, obj basedb.Obj) error {
		var od OperatorData
		if err := json.Unmarshal(obj.Value, &od); err != nil {
			return err
		}
		if (od.ID >= from && od.ID <= to) || (to == 0) {
			operators = append(operators, od)
		}
		return nil
	})

	return operators, err
}

// SaveOperatorData saves operator data
func (s *operatorsStorage) SaveOperatorData(txn basedb.Txn, operatorData *OperatorData) (bool, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, found, err := s.getOperatorData(nil, operatorData.ID)
	if err != nil {
		return found, errors.Wrap(err, "could not get operator data")
	}
	if found {
		s.logger.Debug("operator already exist",
			fields.PubKey(operatorData.PublicKey),
			zap.Uint64("index", operatorData.ID))
		return found, nil
	}

	raw, err := json.Marshal(operatorData)
	if err != nil {
		return found, errors.Wrap(err, "could not marshal operator data")
	}

	setter := s.db.Set
	if txn != nil {
		setter = txn.Set
	}

	return found, setter(s.prefix, buildOperatorKey(operatorData.ID), raw)
}

func (s *operatorsStorage) DeleteOperatorData(txn basedb.Txn, id spectypes.OperatorID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	deleter := s.db.Delete
	if txn != nil {
		deleter = txn.Delete
	}

	return deleter(s.prefix, buildOperatorKey(id))
}

// buildOperatorKey builds operator key using operatorsPrefix & index, e.g. "operators/1"
func buildOperatorKey(id spectypes.OperatorID) []byte {
	return bytes.Join([][]byte{operatorsPrefix, []byte(strconv.FormatUint(id, 10))}, []byte("/"))
}
