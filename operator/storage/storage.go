package storage

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/common"
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
	registrystorage.RecipientsCollection

	GetPrivateKey() (*rsa.PrivateKey, bool, error)
	SetupPrivateKey(logger *zap.Logger, operatorKeyBase64 string, generateIfNone bool) ([]byte, error)
}

type storage struct {
	db basedb.IDb

	operatorStore  registrystorage.OperatorsCollection
	recipientStore registrystorage.RecipientsCollection
}

// NewNodeStorage creates a new instance of Storage
func NewNodeStorage(db basedb.IDb) Storage {
	return &storage{
		db:             db,
		operatorStore:  registrystorage.NewOperatorsStorage(db, storagePrefix),
		recipientStore: registrystorage.NewRecipientsStorage(db, storagePrefix),
	}
}

func (s *storage) GetOperatorDataByPubKey(logger *zap.Logger, operatorPubKey []byte) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorDataByPubKey(logger, operatorPubKey)
}

func (s *storage) GetOperatorData(id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorData(id)
}

func (s *storage) SaveOperatorData(logger *zap.Logger, operatorData *registrystorage.OperatorData) error {
	return s.operatorStore.SaveOperatorData(logger, operatorData)
}

func (s *storage) DeleteOperatorData(id spectypes.OperatorID) error {
	return s.operatorStore.DeleteOperatorData(id)
}

func (s *storage) ListOperators(logger *zap.Logger, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	return s.operatorStore.ListOperators(logger, from, to)
}

func (s *storage) GetOperatorsPrefix() []byte {
	return s.operatorStore.GetOperatorsPrefix()
}

func (s *storage) GetRecipientData(owner common.Address) (*registrystorage.RecipientData, bool, error) {
	return s.recipientStore.GetRecipientData(owner)
}

func (s *storage) SaveRecipientData(recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	return s.recipientStore.SaveRecipientData(recipientData)
}

func (s *storage) DeleteRecipientData(owner common.Address) error {
	return s.recipientStore.DeleteRecipientData(owner)
}

func (s *storage) GetRecipientsPrefix() []byte {
	return s.recipientStore.GetRecipientsPrefix()
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

	err = s.cleanRecipients()
	if err != nil {
		return errors.Wrap(err, "could not clean recipients")
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

func (s *storage) cleanRecipients() error {
	recipientsPrefix := s.GetRecipientsPrefix()
	return s.db.RemoveAllByCollection(append(storagePrefix, recipientsPrefix...))
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
func (s *storage) SetupPrivateKey(logger *zap.Logger, operatorKeyBase64 string, generateIfNone bool) ([]byte, error) {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	if err := s.validateKey(generateIfNone, operatorKey); err != nil {
		return nil, err
	}

	sk, found, err := s.GetPrivateKey()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get operator private key")
	}
	if !found {
		return nil, errors.New("failed to find operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract operator public key")
	}
	//TODO change the log to generated/loaded private key to indicate better on the action
	logger.Info("setup operator privateKey is DONE!", zap.Any("public-key", operatorPublicKey))
	return []byte(operatorPublicKey), nil
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
