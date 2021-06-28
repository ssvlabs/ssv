package exporter

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math"
)

var (
	storagePrefix   = []byte("exporter/")
	operatorsPrefix = []byte("operators")
	syncOffsetKey   = []byte("syncOffset")
)

// Storage represents the interface of exporter storage
type Storage interface {
	eth1.SyncOffsetStorage

	GetOperatorInformation(operatorPubKey []byte) (*api.OperatorInformation, error)
	SaveOperatorInformation(operatorInformation *api.OperatorInformation) error
	ListOperators(from int64, to int64) ([]api.OperatorInformation, error)
}

type exporterStorage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewExporterStorage creates a new instance of Storage
func NewExporterStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := exporterStorage{db, logger.With(zap.String("component", "exporter/storage"))}
	return &es
}

// SaveSyncOffset saves the offset
func (es *exporterStorage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return es.db.Set(storagePrefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (es *exporterStorage) GetSyncOffset() (*eth1.SyncOffset, error) {
	obj, err := es.db.Get(storagePrefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(eth1.SyncOffset)
	offset.SetBytes(obj.Value)
	return offset, nil
}

// ListOperators returns information of all the known operators
// when 'to' equals zero, all operators will be returned
func (es *exporterStorage) ListOperators(from int64, to int64) ([]api.OperatorInformation, error) {
	objs, err := es.db.GetAllByCollection(append(storagePrefix, operatorsPrefix...))
	if err != nil {
		return nil, err
	}
	if to == 0 {
		to = math.MaxInt64
	}
	var operators []api.OperatorInformation
	for _, obj := range objs {
		var oi api.OperatorInformation
		err = json.Unmarshal(obj.Value, &oi)
		if oi.Index >= from && oi.Index <= to {
			operators = append(operators, oi)
		}
	}
	return operators, err
}

// GetOperatorInformation returns information of the given operator by public key
func (es *exporterStorage) GetOperatorInformation(operatorPubKey []byte) (*api.OperatorInformation, error) {
	obj, err := es.db.Get(storagePrefix, operatorKey(operatorPubKey))
	if err != nil {
		return nil, err
	}
	var operatorInformation api.OperatorInformation
	err = json.Unmarshal(obj.Value, &operatorInformation)
	return &operatorInformation, err
}

// SaveOperatorInformation saves operator information by its public key
func (es *exporterStorage) SaveOperatorInformation(operatorInformation *api.OperatorInformation) error {
	existing, err := es.GetOperatorInformation(operatorInformation.PublicKey)
	if err != nil && err.Error() != kv.EntryNotFoundError {
		return errors.Wrap(err, "could not read information from DB")
	}
	if existing != nil {
		es.logger.Debug("operator already exist",
			zap.String("pubKey", hex.EncodeToString(operatorInformation.PublicKey)))
		operatorInformation.Index = existing.Index
		// TODO: update operator information (i.e. other fields such aas "name") for updating operator scenario
		return nil
	}
	operatorInformation.Index, err = es.nextOperatorIndex()
	if err != nil {
		return errors.Wrap(err, "could not calculate next operator index")
	}
	raw, err := json.Marshal(operatorInformation)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
	}
	return es.db.Set(storagePrefix, operatorKey(operatorInformation.PublicKey), raw)
}

// nextOperatorIndex returns the next index for operator
func (es *exporterStorage) nextOperatorIndex() (int64, error) {
	n, err := es.db.CountByCollection(append(storagePrefix, operatorsPrefix...))
	if err != nil {
		return 0, err
	}
	return n, err
}

func operatorKey(pubKey []byte) []byte {
	return bytes.Join([][]byte{
		operatorsPrefix[:],
		pubKey[:],
	}, []byte("/"))
}
