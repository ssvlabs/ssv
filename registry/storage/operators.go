package storage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	operatorsPrefix = []byte("operators")
)

// OperatorData the public data of an operator
type OperatorData struct {
	Index        uint64         `json:"index"`
	PublicKey    string         `json:"publicKey"`
	OwnerAddress common.Address `json:"ownerAddress"`
}

// GetOperatorData is a function that returns the operator data
type GetOperatorData = func(index uint64) (*OperatorData, bool, error)

// OperatorsCollection is the interface for managing operators data
type OperatorsCollection interface {
	GetOperatorDataByPubKey(logger *zap.Logger, operatorPubKey string) (*OperatorData, bool, error)
	GetOperatorData(index uint64) (*OperatorData, bool, error)
	SaveOperatorData(logger *zap.Logger, operatorData *OperatorData) error
	DeleteOperatorData(index uint64) error
	ListOperators(logger *zap.Logger, from uint64, to uint64) ([]OperatorData, error)
	GetOperatorsPrefix() []byte
}

type operatorsStorage struct {
	db     basedb.IDb
	lock   sync.RWMutex
	prefix []byte
}

// NewOperatorsStorage creates a new instance of Storage
func NewOperatorsStorage(db basedb.IDb, prefix []byte) OperatorsCollection {
	return &operatorsStorage{
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
func (s *operatorsStorage) ListOperators(logger *zap.Logger, from, to uint64) ([]OperatorData, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.listOperators(logger, from, to)
}

// GetOperatorData returns data of the given operator by index
func (s *operatorsStorage) GetOperatorData(index uint64) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorData(index)
}

// GetOperatorDataByPubKey returns data of the given operator by public key
func (s *operatorsStorage) GetOperatorDataByPubKey(logger *zap.Logger, operatorPubKey string) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorDataByPubKey(logger, operatorPubKey)
}

func (s *operatorsStorage) getOperatorDataByPubKey(logger *zap.Logger, operatorPubKey string) (*OperatorData, bool, error) {
	operatorsData, err := s.listOperators(logger, 0, 0)
	if err != nil {
		return nil, false, errors.Wrap(err, "could not get all operators")
	}
	for _, op := range operatorsData {
		if strings.EqualFold(op.PublicKey, operatorPubKey) {
			return &op, true, nil
		}
	}
	return nil, false, nil
}

func (s *operatorsStorage) getOperatorData(index uint64) (*OperatorData, bool, error) {
	obj, found, err := s.db.Get(s.prefix, buildOperatorKey(index))
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

func (s *operatorsStorage) listOperators(logger *zap.Logger, from, to uint64) ([]OperatorData, error) {
	var operators []OperatorData
	err := s.db.GetAll(logger, append(s.prefix, operatorsPrefix...), func(i int, obj basedb.Obj) error {
		var od OperatorData
		if err := json.Unmarshal(obj.Value, &od); err != nil {
			return err
		}
		if (od.Index >= from && od.Index <= to) || (to == 0) {
			operators = append(operators, od)
		}
		return nil
	})

	return operators, err
}

// SaveOperatorData saves operator data
func (s *operatorsStorage) SaveOperatorData(logger *zap.Logger, operatorData *OperatorData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, found, err := s.getOperatorData(operatorData.Index)
	if err != nil {
		return errors.Wrap(err, "could not get operator data")
	}
	if found {
		logger.Debug("operator already exist",
			zap.String("pubKey", operatorData.PublicKey),
			zap.Uint64("index", operatorData.Index))
		return nil
	}

	raw, err := json.Marshal(operatorData)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator data")
	}
	return s.db.Set(s.prefix, buildOperatorKey(operatorData.Index), raw)
}

func (s *operatorsStorage) DeleteOperatorData(index uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.prefix, buildOperatorKey(index))
}

// buildOperatorKey builds operator key using operatorsPrefix & index, e.g. "operators/1"
func buildOperatorKey(index uint64) []byte {
	return bytes.Join([][]byte{operatorsPrefix, []byte(strconv.FormatUint(index, 10))}, []byte("/"))
}
