package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	Name         string         `json:"name"`
	OwnerAddress common.Address `json:"ownerAddress"`
}

// GetOperatorData is a function that returns the operator data
type GetOperatorData = func(index uint64) (*OperatorData, bool, error)

// OperatorsCollection is the interface for managing operators data
type OperatorsCollection interface {
	GetOperatorDataByPubKey(operatorPubKey string) (*OperatorData, bool, error)
	GetOperatorData(index uint64) (*OperatorData, bool, error)
	SaveOperatorData(operatorData *OperatorData) error
	DeleteOperatorData(index uint64) error
	ListOperators(from uint64, to uint64) ([]OperatorData, error)
	GetOperatorsPrefix() []byte
}

type operatorsStorage struct {
	db     basedb.IDb
	logger *zap.Logger
	lock   sync.RWMutex
	prefix []byte
}

// NewOperatorsStorage creates a new instance of Storage
func NewOperatorsStorage(db basedb.IDb, logger *zap.Logger, prefix []byte) OperatorsCollection {
	return &operatorsStorage{
		db:     db,
		logger: logger.With(zap.String("component", fmt.Sprintf("%sstorage", prefix))),
		prefix: prefix,
	}
}

// GetOperatorsPrefix returns the prefix
func (s *operatorsStorage) GetOperatorsPrefix() []byte {
	return operatorsPrefix
}

// ListOperators returns data of the all known operators by index range (from, to)
// when 'to' equals zero, all operators will be returned
func (s *operatorsStorage) ListOperators(from, to uint64) ([]OperatorData, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.listOperators(from, to)
}

// GetOperatorData returns data of the given operator by index
func (s *operatorsStorage) GetOperatorData(index uint64) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorData(index)
}

// GetOperatorDataByPubKey returns data of the given operator by public key
func (s *operatorsStorage) GetOperatorDataByPubKey(operatorPubKey string) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorDataByPubKey(operatorPubKey)
}

func (s *operatorsStorage) getOperatorDataByPubKey(operatorPubKey string) (*OperatorData, bool, error) {
	operatorsData, err := s.listOperators(0, 0)
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

func (s *operatorsStorage) listOperators(from, to uint64) ([]OperatorData, error) {
	var operators []OperatorData
	err := s.db.GetAll(append(s.prefix, operatorsPrefix...), func(i int, obj basedb.Obj) error {
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
func (s *operatorsStorage) SaveOperatorData(operatorData *OperatorData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if operatorData.Index == 0 {
		nextIndex, err := s.nextIndex()
		if err != nil {
			return errors.Wrap(err, "could not calculate next operator index")
		}
		operatorData.Index = uint64(nextIndex)
	}

	_, found, err := s.getOperatorData(operatorData.Index)
	if err != nil {
		return errors.Wrap(err, "could not get operator's data")
	}
	if found {
		s.logger.Debug("operator already exist",
			zap.String("pubKey", operatorData.PublicKey),
			zap.Uint64("index", operatorData.Index))
		return nil
	}

	raw, err := json.Marshal(operatorData)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
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
	return bytes.Join([][]byte{operatorsPrefix[:], []byte(strconv.FormatUint(index, 10))}, []byte("/"))
}

func (s *operatorsStorage) nextIndex() (int64, error) {
	return s.db.CountByCollection(append(s.prefix, operatorsPrefix...))
}
