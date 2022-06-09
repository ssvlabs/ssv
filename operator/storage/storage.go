package storage

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	registry "github.com/bloxapp/ssv/protocol/v1/blockchain/eth1"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

var (
	storagePrefix = []byte("operator-")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	eth1.SyncOffsetStorage
	registry.RegistryStore
	registrystorage.OperatorsCollection
	ValidatorsCollection

	GetPrivateKey() (*rsa.PrivateKey, bool, error)
	SetupPrivateKey(generateIfNone bool, operatorKeyBase64 string) error
}

type storage struct {
	db     basedb.IDb
	logger *zap.Logger

	validatorsLock sync.RWMutex

	operatorStore registrystorage.OperatorsCollection
}

// NewNodeStorage creates a new instance of Storage
func NewNodeStorage(db basedb.IDb, logger *zap.Logger) Storage {
	return &storage{
		db:            db,
		logger:        logger,
		operatorStore: registrystorage.NewOperatorsStorage(db, logger, storagePrefix),
	}
}

func (s *storage) GetOperatorDataByPubKey(operatorPubKey string) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorDataByPubKey(operatorPubKey)
}

func (s *storage) GetOperatorData(index uint64) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorData(index)
}

func (s *storage) SaveOperatorData(operatorData *registrystorage.OperatorData) error {
	return s.operatorStore.SaveOperatorData(operatorData)
}

func (s *storage) ListOperators(from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	return s.operatorStore.ListOperators(from, to)
}

func (s *storage) GetOperatorsPrefix() []byte {
	return s.operatorStore.GetOperatorsPrefix()
}

func (s *storage) CleanRegistryData() error {
	err := s.cleanSyncOffset()
	if err != nil {
		return errors.Wrap(err, "could not clean sync offset")
	}

	err = s.cleanOperators()
	if err != nil {
		return errors.Wrap(err, "could not clean operators")
	}
	return nil
}

// SaveSyncOffset saves the offset
func (s *storage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return s.db.Set(storagePrefix, syncOffsetKey, offset.Bytes())
}

func (s *storage) cleanSyncOffset() error {
	return s.db.RemoveAllByCollection(append(storagePrefix, syncOffsetKey...))
}

func (s *storage) cleanOperators() error {
	operatorsPrefix := s.GetOperatorsPrefix()
	return s.db.RemoveAllByCollection(append(storagePrefix, operatorsPrefix...))
}

// GetSyncOffset returns the offset
func (s *storage) GetSyncOffset() (*eth1.SyncOffset, bool, error) {
	obj, found, err := s.db.Get(storagePrefix, syncOffsetKey)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, found, nil
}

// GetPrivateKey return rsa private key
func (s *storage) GetPrivateKey() (*rsa.PrivateKey, bool, error) {
	obj, found, err := s.db.Get(storagePrefix, []byte("private-key"))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, found, nil
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(obj.Value))
	if err != nil {
		return nil, false, err
	}
	return sk, found, nil
}

// SetupPrivateKey setup operator private key at the init of the node and set OperatorPublicKey config
func (s *storage) SetupPrivateKey(generateIfNone bool, operatorKeyBase64 string) error {
	logger := s.logger.With(zap.String("who", "operatorKeys"))
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	if err := s.validateKey(generateIfNone, operatorKey); err != nil {
		return err
	}

	sk, found, err := s.GetPrivateKey()
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}
	if !found {
		return errors.New("failed to find operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return errors.Wrap(err, "failed to extract operator public key")
	}
	logger.Info("setup operator privateKey is DONE!", zap.Any("public-key", operatorPublicKey))
	return nil
}

// validateKey validate provided and exist key. save if needed.
func (s *storage) validateKey(generateIfNone bool, operatorKey string) error {
	// check if passed new key. if so, save new key (force to always save key when provided)
	if operatorKey != "" {
		return s.savePrivateKey(operatorKey)
	}
	// new key not provided, check if key exist
	_, found, err := s.GetPrivateKey()
	if err != nil {
		return err
	}
	// if no, check if need to generate. if no, return error
	if !found {
		if !generateIfNone {
			return errors.New("key not exist or provided")
		}
		_, skByte, err := rsaencryption.GenerateKeys()
		if err != nil {
			return errors.WithMessage(err, "failed to generate new key")
		}
		return s.savePrivateKey(string(skByte))
	}

	// key exist in storage.
	return nil
}

// SavePrivateKey save operator private key
func (s *storage) savePrivateKey(operatorKey string) error {
	if err := s.db.Set(storagePrefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}

// nextIndex returns the next index for operator
func (s *storage) nextIndex(prefix []byte) (int64, error) {
	n, err := s.db.CountByCollection(append(storagePrefix, prefix...))
	if err != nil {
		return 0, err
	}
	return n, err
}
