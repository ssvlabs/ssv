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

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

var (
	storagePrefix         = []byte("operator/")
	lastProcessedBlockKey = []byte("syncOffset") // TODO: temporarily left as syncOffset for compatibility, consider renaming
)

// Storage represents the interface for ssv node storage
type Storage interface {
	// TODO: de-anonymize the sub-storages, like Shares() below

	ROTxn() basedb.Reader
	RWTxn() basedb.Txn

	SaveLastProcessedBlock(txn basedb.Txn, offset *big.Int) error
	GetLastProcessedBlock(txn basedb.Txn) (*big.Int, bool, error)

	registry.RegistryStore

	registrystorage.Operators
	registrystorage.Recipients
	Shares() registrystorage.Shares
	registrystorage.Events

	GetPrivateKey() (*rsa.PrivateKey, bool, error)
	SetupPrivateKey(operatorKeyBase64 string, generateIfNone bool) ([]byte, error)
}

type storage struct {
	logger *zap.Logger
	db     basedb.Database

	operatorStore  registrystorage.Operators
	recipientStore registrystorage.Recipients
	shareStore     registrystorage.Shares
	eventStore     registrystorage.Events
}

// NewNodeStorage creates a new instance of Storage
func NewNodeStorage(logger *zap.Logger, db basedb.Database) (Storage, error) {
	stg := &storage{
		logger:         logger,
		db:             db,
		operatorStore:  registrystorage.NewOperatorsStorage(logger, db, storagePrefix),
		recipientStore: registrystorage.NewRecipientsStorage(logger, db, storagePrefix),
		eventStore:     registrystorage.NewEventsStorage(logger, db, storagePrefix),
	}
	var err error
	stg.shareStore, err = registrystorage.NewSharesStorage(logger, db, storagePrefix)
	if err != nil {
		return nil, err
	}
	return stg, nil
}

func (s *storage) ROTxn() basedb.Reader {
	return s.db.ROTxn()
}

func (s *storage) RWTxn() basedb.Txn {
	return s.db.RWTxn()
}

func (s *storage) Shares() registrystorage.Shares {
	return s.shareStore
}

func (s *storage) GetOperatorDataByPubKey(txn basedb.Txn, operatorPubKey []byte) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorDataByPubKey(txn, operatorPubKey)
}

func (s *storage) GetOperatorData(txn basedb.Txn, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorData(txn, id)
}

func (s *storage) SaveOperatorData(txn basedb.Txn, operatorData *registrystorage.OperatorData) (bool, error) {
	return s.operatorStore.SaveOperatorData(txn, operatorData)
}

func (s *storage) DeleteOperatorData(txn basedb.Txn, id spectypes.OperatorID) error {
	return s.operatorStore.DeleteOperatorData(txn, id)
}

func (s *storage) ListOperators(txn basedb.Txn, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	return s.operatorStore.ListOperators(txn, from, to)
}

func (s *storage) GetOperatorsPrefix() []byte {
	return s.operatorStore.GetOperatorsPrefix()
}

func (s *storage) GetRecipientData(txn basedb.Txn, owner common.Address) (*registrystorage.RecipientData, bool, error) {
	return s.recipientStore.GetRecipientData(txn, owner)
}

func (s *storage) GetRecipientDataMany(txn basedb.Txn, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	return s.recipientStore.GetRecipientDataMany(txn, owners)
}

func (s *storage) SaveRecipientData(txn basedb.Txn, recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	return s.recipientStore.SaveRecipientData(txn, recipientData)
}

func (s *storage) DeleteRecipientData(txn basedb.Txn, owner common.Address) error {
	return s.recipientStore.DeleteRecipientData(txn, owner)
}

func (s *storage) GetNextNonce(txn basedb.Txn, owner common.Address) (registrystorage.Nonce, error) {
	return s.recipientStore.GetNextNonce(txn, owner)
}

func (s *storage) BumpNonce(txn basedb.Txn, owner common.Address) error {
	return s.recipientStore.BumpNonce(txn, owner)
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
	err := s.cleanLastProcessedBlock()
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

// TODO: review what's not needed anymore and delete

func (s *storage) SaveLastProcessedBlock(txn basedb.Txn, offset *big.Int) error {
	setter := s.db.Set
	if txn != nil {
		setter = txn.Set
	}

	return setter(storagePrefix, lastProcessedBlockKey, offset.Bytes())
}

func (s *storage) cleanLastProcessedBlock() error {
	return s.db.RemoveAllByCollection(append(storagePrefix, lastProcessedBlockKey...))
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

// GetLastProcessedBlock returns the last processed block.
func (s *storage) GetLastProcessedBlock(txn basedb.Txn) (*big.Int, bool, error) {
	getter := s.db.Get
	if txn != nil {
		getter = txn.Get
	}

	obj, found, err := getter(storagePrefix, lastProcessedBlockKey)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}

	offset := new(big.Int).SetBytes(obj.Value)
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
func (s *storage) SetupPrivateKey(operatorKeyBase64 string, generateIfNone bool) ([]byte, error) {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to decode base64")
	}

	if err := s.validateKey(generateIfNone, string(operatorKeyByte)); err != nil {
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
	s.logger.Info("setup operator key is DONE!", zap.Any("public-key", operatorPublicKey))
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

func (s *storage) UpdateValidatorMetadata(pk string, metadata *beacon.ValidatorMetadata) error {
	return s.shareStore.UpdateValidatorMetadata(pk, metadata)
}
