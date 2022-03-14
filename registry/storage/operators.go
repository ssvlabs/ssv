package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	operatorsPrefix = []byte("operators")
)

// OperatorInformation the public data of an operator
type OperatorInformation struct {
	PublicKey    string         `json:"publicKey"`
	Name         string         `json:"name"`
	OwnerAddress common.Address `json:"ownerAddress"`
	Index        int64          `json:"index"`
}

// OperatorsCollection is the interface for managing operators information
type OperatorsCollection interface {
	GetOperatorInformation(operatorPubKey string) (*OperatorInformation, bool, error)
	SaveOperatorInformation(operatorInformation *OperatorInformation) error
	ListOperators(from int64, to int64) ([]OperatorInformation, error)
	GetOperatorsPrefix() []byte
}

type operatorsStorage struct {
	db            basedb.IDb
	logger        *zap.Logger
	operatorsLock sync.RWMutex
	prefix        []byte
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
	return s.prefix
}

// ListOperators returns information of all the known operators
// when 'to' equals zero, all operators will be returned
func (s *operatorsStorage) ListOperators(from int64, to int64) ([]OperatorInformation, error) {
	s.operatorsLock.RLock()
	defer s.operatorsLock.RUnlock()

	var operators []OperatorInformation
	to = normalTo(to)
	err := s.db.GetAll(append(s.prefix, operatorsPrefix...), func(i int, obj basedb.Obj) error {
		var oi OperatorInformation
		if err := json.Unmarshal(obj.Value, &oi); err != nil {
			return err
		}
		if oi.Index >= from && oi.Index <= to {
			operators = append(operators, oi)
		}
		return nil
	})

	return operators, err
}

// GetOperatorInformation returns information of the given operator by public key
func (s *operatorsStorage) GetOperatorInformation(operatorPubKey string) (*OperatorInformation, bool, error) {
	s.operatorsLock.RLock()
	defer s.operatorsLock.RUnlock()

	return s.getOperatorInformation(operatorPubKey)
}

// GetOperatorInformation returns information of the given operator by public key
func (s *operatorsStorage) getOperatorInformation(operatorPubKey string) (*OperatorInformation, bool, error) {
	obj, found, err := s.db.Get(s.prefix, operatorKey(operatorPubKey))
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	var operatorInformation OperatorInformation
	err = json.Unmarshal(obj.Value, &operatorInformation)
	return &operatorInformation, found, err
}

// SaveOperatorInformation saves operator information by its public key
func (s *operatorsStorage) SaveOperatorInformation(operatorInformation *OperatorInformation) error {
	s.operatorsLock.Lock()
	defer s.operatorsLock.Unlock()

	info, found, err := s.getOperatorInformation(operatorInformation.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not read information from DB")
	}
	if found {
		s.logger.Debug("operator already exist",
			zap.String("pubKey", operatorInformation.PublicKey))
		operatorInformation.Index = info.Index
		// TODO: update operator information (i.e. change name)
		return nil
	}

	operatorInformation.Index, err = s.nextIndex(operatorsPrefix)
	if err != nil {
		return errors.Wrap(err, "could not calculate next operator index")
	}
	raw, err := json.Marshal(operatorInformation)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
	}
	return s.db.Set(s.prefix, operatorKey(operatorInformation.PublicKey), raw)
}

func (s *operatorsStorage) nextIndex(prefix []byte) (int64, error) {
	return s.db.CountByCollection(append(s.prefix, prefix...))
}

func operatorKey(pubKey string) []byte {
	return bytes.Join([][]byte{
		operatorsPrefix[:],
		[]byte(pubKey),
	}, []byte("/"))
}

func normalTo(to int64) int64 {
	if to == 0 {
		return math.MaxInt64
	}
	return to
}
