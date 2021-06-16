package exporter

import (
	"bytes"
	"encoding/json"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	storagePrefix = []byte("exporter/")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface of exporter storage
type Storage interface {
	eth1.SyncOffsetStorage

	GetOperatorInformation(operatorPubKey []byte) (*OperatorInformation, error)
	SaveOperatorInformation(operatorInformation *OperatorInformation) error
	ListOperators() ([]OperatorInformation, error)
}

// OperatorInformation the public data of an operator
type OperatorInformation struct {
	PublicKey    []byte
	Name         string
	OwnerAddress common.Address
	Index        int
}

type exporterStorage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewExporterStorage creates a new instance of Storage
func NewExporterStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := exporterStorage{db, logger}
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
func (es *exporterStorage) ListOperators() ([]OperatorInformation, error) {
	objs, err := es.db.GetAllByCollection(storagePrefix)
	if err != nil {
		return nil, err
	}
	var operators []OperatorInformation
	privKeyBytes := append(storagePrefix, []byte("private-key")...)
	for _, obj := range objs {
		if !bytes.Equal(obj.Key, privKeyBytes) {
			var operatorInformation OperatorInformation
			err = json.Unmarshal(obj.Value, &operatorInformation)
			operators = append(operators, operatorInformation)
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
	raw, err := json.Marshal(operatorInformation)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
	}
	return es.db.Set(storagePrefix, operatorKey(operatorInformation.PublicKey), raw)
}

func operatorKey(pubKey []byte) []byte {
	return bytes.Join([][]byte{
		[]byte("operator/"),
		pubKey[:],
	}, []byte("/"))
}
