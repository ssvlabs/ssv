package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/v2/observability/log/fields"
	"github.com/ssvlabs/ssv/v2/storage/basedb"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/operators.go -source=./operators.go

var (
	operatorsPrefix = []byte("operators")
)

// OperatorData the public data of an operator
type OperatorData struct {
	ID           spectypes.OperatorID `json:"id"`
	PublicKey    string               `json:"publicKey"` // stored as base64 string
	OwnerAddress common.Address       `json:"ownerAddress"`
}

// MarshalJSON marshals OperatorData with PublicKey as []byte to match the DB format.
func (o OperatorData) MarshalJSON() ([]byte, error) {
	type Alias OperatorData
	return json.Marshal(&struct {
		PublicKey []byte `json:"publicKey"` // still emit as []byte (JSON string)
		Alias
	}{
		PublicKey: []byte(o.PublicKey),
		Alias:     (Alias)(o),
	})
}

// UnmarshalJSON unmarshals OperatorData with PublicKey as []byte to match the DB format.
func (o *OperatorData) UnmarshalJSON(data []byte) error {
	type Alias OperatorData
	aux := &struct {
		PublicKey []byte `json:"publicKey"` // read as []byte (JSON string)
		*Alias
	}{
		Alias: (*Alias)(o),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	o.PublicKey = string(aux.PublicKey)
	return nil
}

// GetOperatorData is a function that returns the operator data
type GetOperatorData = func(index uint64) (*OperatorData, bool, error)

// Operators is the interface for managing operators data
type Operators interface {
	GetOperatorDataByPubKey(r basedb.Reader, operatorPubKey string) (*OperatorData, bool, error)
	GetOperatorData(r basedb.Reader, id spectypes.OperatorID) (*OperatorData, bool, error)
	OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error)
	SaveOperatorData(rw basedb.ReadWriter, operatorData *OperatorData) (bool, error)
	DeleteOperatorData(rw basedb.ReadWriter, id spectypes.OperatorID) error
	ListOperatorsAll(r basedb.Reader) ([]OperatorData, error)
	ListOperators(r basedb.Reader, from uint64, to uint64) ([]OperatorData, error)
	GetOperatorsPrefix() []byte
	DropOperators() error
}

type operatorsStorage struct {
	logger    *zap.Logger
	db        basedb.Database
	lock      sync.RWMutex
	prefix    []byte
	pubkeyIdx map[string]spectypes.OperatorID
}

// NewOperatorsStorage creates a new instance of Storage and loads the pubkey index from existing data.
func NewOperatorsStorage(logger *zap.Logger, db basedb.Database, prefix []byte) (Operators, error) {
	s := &operatorsStorage{
		logger:    logger,
		db:        db,
		prefix:    prefix,
		pubkeyIdx: make(map[string]spectypes.OperatorID),
	}

	operators, err := s.listOperators(nil, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	for _, op := range operators {
		s.pubkeyIdx[op.PublicKey] = op.ID
	}

	return s, nil
}

// GetOperatorsPrefix returns DB prefix
func (s *operatorsStorage) GetOperatorsPrefix() []byte {
	return operatorsPrefix
}

// ListOperatorsAll returns data of the all known operators.
func (s *operatorsStorage) ListOperatorsAll(r basedb.Reader) ([]OperatorData, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.listOperators(r, 0, 0)
}

// ListOperators returns data of the all known operators by index range (from, to)
// when 'to' equals zero, all operators will be returned
func (s *operatorsStorage) ListOperators(r basedb.Reader, from uint64, to uint64) ([]OperatorData, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.listOperators(r, from, to)
}

// GetOperatorData returns data of the given operator by index
func (s *operatorsStorage) GetOperatorData(
	r basedb.Reader,
	id spectypes.OperatorID,
) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorData(r, id)
}

// OperatorsExist returns if operators exist
func (s *operatorsStorage) OperatorsExist(
	r basedb.Reader,
	ids []spectypes.OperatorID,
) (bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.operatorsExist(r, ids)
}

// GetOperatorDataByPubKey returns data of the given operator by public key
func (s *operatorsStorage) GetOperatorDataByPubKey(r basedb.Reader, operatorPubKey string) (*OperatorData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getOperatorDataByPubKey(r, operatorPubKey)
}

func (s *operatorsStorage) getOperatorDataByPubKey(
	r basedb.Reader,
	operatorPubKey string,
) (*OperatorData, bool, error) {
	id, ok := s.pubkeyIdx[operatorPubKey]
	if !ok {
		return nil, false, nil
	}
	op, found, err := s.getOperatorData(r, id)
	if err != nil {
		return nil, false, err
	}
	if !found || op.PublicKey != operatorPubKey {
		return nil, false, nil
	}
	return op, true, nil
}

func (s *operatorsStorage) getOperatorData(
	r basedb.Reader,
	id spectypes.OperatorID,
) (*OperatorData, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(s.prefix, buildOperatorKey(id))
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

func (s *operatorsStorage) operatorsExist(
	r basedb.Reader,
	ids []spectypes.OperatorID,
) (bool, error) {
	keys := make([][]byte, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, buildOperatorKey(id))
	}

	seen := 0
	err := s.db.UsingReader(r).GetMany(s.prefix, keys, func(obj basedb.Obj) error {
		seen++
		return nil
	})
	if err != nil {
		return false, err
	}

	return seen == len(ids), nil
}

func (s *operatorsStorage) listOperators(r basedb.Reader, from, to uint64) ([]OperatorData, error) {
	var operators []OperatorData
	err := s.db.UsingReader(r).
		GetAll(append(s.prefix, operatorsPrefix...), func(i int, obj basedb.Obj) error {
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
func (s *operatorsStorage) SaveOperatorData(
	rw basedb.ReadWriter,
	operatorData *OperatorData,
) (bool, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, found, err := s.getOperatorData(nil, operatorData.ID)
	if err != nil {
		return found, fmt.Errorf("get operator data: %w", err)
	}
	if found {
		s.logger.Debug("operator already exist",
			fields.OperatorPubKey(operatorData.PublicKey),
			zap.Uint64("index", operatorData.ID))
		return found, nil
	}

	raw, err := json.Marshal(operatorData)
	if err != nil {
		return found, fmt.Errorf("marshal operator data: %w", err)
	}

	if err := s.db.Using(rw).Set(s.prefix, buildOperatorKey(operatorData.ID), raw); err != nil {
		return found, fmt.Errorf("set operator data: %w", err)
	}

	s.pubkeyIdx[operatorData.PublicKey] = operatorData.ID

	return found, nil
}

func (s *operatorsStorage) DeleteOperatorData(rw basedb.ReadWriter, id spectypes.OperatorID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Using(rw).Delete(s.prefix, buildOperatorKey(id))
}

func (s *operatorsStorage) DropOperators() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.DropPrefix(bytes.Join(
		[][]byte{s.prefix, operatorsPrefix, []byte("/")},
		nil,
	)); err != nil {
		return err
	}

	s.pubkeyIdx = make(map[string]spectypes.OperatorID)

	return nil
}

// buildOperatorKey builds operator key using operatorsPrefix & index, e.g. "operators/1"
func buildOperatorKey(id spectypes.OperatorID) []byte {
	return bytes.Join([][]byte{operatorsPrefix, []byte(strconv.FormatUint(id, 10))}, []byte("/"))
}
