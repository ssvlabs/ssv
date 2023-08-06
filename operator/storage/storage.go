package storage

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

var HashedPrivateKey = "hashed-private-key"

var (
	storagePrefix = []byte("operator/")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	// TODO: de-anonymize the sub-storages, like Shares() below

	eth1.SyncOffsetStorage
	registry.RegistryStore

	registrystorage.Operators
	registrystorage.Recipients
	Shares() registrystorage.Shares
	registrystorage.Events

	GetPrivateKey() (*rsa.PrivateKey, bool, error)
	SetupPrivateKey(logger *zap.Logger, operatorKeyBase64 string, generateIfNone bool) ([]byte, error)
}

type storage struct {
	db basedb.IDb

	operatorPrivateKey []byte
	operatorStore      registrystorage.Operators
	recipientStore     registrystorage.Recipients
	shareStore         registrystorage.Shares
	eventStore         registrystorage.Events
}

// NewNodeStorage creates a new instance of Storage
func NewNodeStorage(logger *zap.Logger, db basedb.IDb) (Storage, error) {
	stg := &storage{
		db:             db,
		operatorStore:  registrystorage.NewOperatorsStorage(db, storagePrefix),
		recipientStore: registrystorage.NewRecipientsStorage(db, storagePrefix),
		eventStore:     registrystorage.NewEventsStorage(db, storagePrefix),
	}
	var err error
	stg.shareStore, err = registrystorage.NewSharesStorage(logger, db, storagePrefix)
	if err != nil {
		return nil, err
	}
	return stg, nil
}

func (s *storage) Shares() registrystorage.Shares {
	return s.shareStore
}

func (s *storage) GetOperatorDataByPubKey(logger *zap.Logger, operatorPubKey []byte) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorDataByPubKey(logger, operatorPubKey)
}

func (s *storage) GetOperatorData(id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorData(id)
}

func (s *storage) SaveOperatorData(logger *zap.Logger, operatorData *registrystorage.OperatorData) (bool, error) {
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

func (s *storage) GetRecipientDataMany(logger *zap.Logger, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	return s.recipientStore.GetRecipientDataMany(logger, owners)
}

func (s *storage) SaveRecipientData(recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	return s.recipientStore.SaveRecipientData(recipientData)
}

func (s *storage) DeleteRecipientData(owner common.Address) error {
	return s.recipientStore.DeleteRecipientData(owner)
}

func (s *storage) GetNextNonce(owner common.Address) (registrystorage.Nonce, error) {
	return s.recipientStore.GetNextNonce(owner)
}

func (s *storage) BumpNonce(owner common.Address) error {
	return s.recipientStore.BumpNonce(owner)
}

func (s *storage) GetRecipientsPrefix() []byte {
	return s.recipientStore.GetRecipientsPrefix()
}

func (s *storage) GetEventData(txHash common.Hash) (*registrystorage.EventData, bool, error) {
	return s.eventStore.GetEventData(txHash)
}

func (s *storage) SaveEventData(txHash common.Hash) error {
	return s.eventStore.SaveEventData(txHash)
}

func (s *storage) GetEventsPrefix() []byte {
	return s.eventStore.GetEventsPrefix()
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

	err = s.cleanEvents()
	if err != nil {
		return errors.Wrap(err, "could not clean events")
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

func (s *storage) cleanEvents() error {
	eventsPrefix := s.GetEventsPrefix()
	return s.db.RemoveAllByCollection(append(storagePrefix, eventsPrefix...))
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

// GetHashedPrivateKey return sha256 hashed private key
func (s *storage) GetHashedPrivateKey() ([]byte, bool, error) {
	obj, found, err := s.db.Get(storagePrefix, []byte(HashedPrivateKey))
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	return obj.Value, found, nil
}

// GetPrivateKey return rsa private key
func (s *storage) GetPrivateKey() (*rsa.PrivateKey, bool, error) {
	privateKey := s.operatorPrivateKey
	if privateKey == nil {
		return nil, false, nil
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(privateKey))
	if err != nil {
		return nil, false, err
	}
	return sk, true, nil
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
	storedPrivateKey, privateKeyExist, err := s.GetHashedPrivateKey()
	if err != nil {
		return errors.New("Can't Get Operator private key from storage")
	}
	hashedKey, err := rsaencryption.HashRsaKey([]byte(operatorKey))
	if err != nil {
		return errors.New("Cannot hash Operator private key")
	}
	if privateKeyExist && hashedKey != string(storedPrivateKey) {
		return errors.New("Operator private key is not matching the one encrypted the storage")
	}
	if operatorKey != "" {
		return s.savePrivateKey(operatorKey)
	}
	// new key not provided, check if key exist
	_, found, err := s.GetPrivateKey()
	if err != nil {
		return err
	}
	// if no, check  if you need to generate. if no, return error
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
	hashedKey, err := rsaencryption.HashRsaKey([]byte(operatorKey))
	if err != nil {
		return err
	}
	if err := s.db.Set(storagePrefix, []byte(HashedPrivateKey), []byte(hashedKey)); err != nil {
		return err
	}
	s.operatorPrivateKey = []byte(operatorKey)
	return nil
}

func (s *storage) UpdateValidatorMetadata(logger *zap.Logger, pk string, metadata *beacon.ValidatorMetadata) error {
	return s.shareStore.(beacon.ValidatorMetadataStorage).UpdateValidatorMetadata(logger, pk, metadata)
}
