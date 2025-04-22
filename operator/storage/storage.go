package storage

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	registry "github.com/ssvlabs/ssv/protocol/v2/blockchain/eth1"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	OperatorStoragePrefix = []byte("operator/")
	lastProcessedBlockKey = []byte("syncOffset") // TODO: temporarily left as syncOffset for compatibility, consider renaming and adding a migration for that
	configKey             = []byte("config")
	hashedPrivkeyDBKey    = "hashed-private-key"
	pubkeyDBKey           = "public-key"
)

// Storage represents the interface for ssv node storage
type Storage interface {
	// TODO: de-anonymize the sub-storages, like Shares() below

	Begin() basedb.Txn
	BeginRead() basedb.ReadTxn

	SaveLastProcessedBlock(rw basedb.ReadWriter, offset *big.Int) error
	GetLastProcessedBlock(r basedb.Reader) (*big.Int, bool, error)

	GetConfig(rw basedb.ReadWriter) (*ConfigLock, bool, error)
	SaveConfig(rw basedb.ReadWriter, config *ConfigLock) error
	DeleteConfig(rw basedb.ReadWriter) error

	registry.RegistryStore

	registrystorage.Operators
	registrystorage.Recipients
	Shares() registrystorage.Shares
	ValidatorStore() registrystorage.ValidatorStore

	GetPrivateKeyHash() (string, bool, error)
	SavePrivateKeyHash(privKeyHash string) error

	GetPublicKey() (string, bool, error)
	SavePublicKey(pubKey string) error
}

type storage struct {
	logger *zap.Logger
	db     basedb.Database

	operatorStore  registrystorage.Operators
	recipientStore registrystorage.Recipients
	shareStore     registrystorage.Shares
	validatorStore registrystorage.ValidatorStore
}

// NewNodeStorage creates a new instance of Storage
func NewNodeStorage(networkConfig networkconfig.NetworkConfig, logger *zap.Logger, db basedb.Database) (Storage, error) {
	stg := &storage{
		logger:         logger,
		db:             db,
		operatorStore:  registrystorage.NewOperatorsStorage(logger, db, OperatorStoragePrefix),
		recipientStore: registrystorage.NewRecipientsStorage(logger, db, OperatorStoragePrefix),
	}

	var err error

	stg.shareStore, stg.validatorStore, err = registrystorage.NewSharesStorage(networkConfig, db, OperatorStoragePrefix)
	if err != nil {
		return nil, err
	}

	return stg, nil
}

func (s *storage) Begin() basedb.Txn {
	return s.db.Begin()
}

func (s *storage) BeginRead() basedb.ReadTxn {
	return s.db.BeginRead()
}

func (s *storage) Shares() registrystorage.Shares {
	return s.shareStore
}

func (s *storage) ValidatorStore() registrystorage.ValidatorStore {
	return s.validatorStore
}

func (s *storage) GetOperatorDataByPubKey(r basedb.Reader, operatorPubKey []byte) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorDataByPubKey(r, operatorPubKey)
}

func (s *storage) GetOperatorData(r basedb.Reader, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	return s.operatorStore.GetOperatorData(r, id)
}

func (s *storage) OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error) {
	return s.operatorStore.OperatorsExist(r, ids)
}

func (s *storage) SaveOperatorData(rw basedb.ReadWriter, operatorData *registrystorage.OperatorData) (bool, error) {
	return s.operatorStore.SaveOperatorData(rw, operatorData)
}

func (s *storage) DeleteOperatorData(rw basedb.ReadWriter, id spectypes.OperatorID) error {
	return s.operatorStore.DeleteOperatorData(rw, id)
}

func (s *storage) ListOperators(r basedb.Reader, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	return s.operatorStore.ListOperators(r, from, to)
}

func (s *storage) GetOperatorsPrefix() []byte {
	return s.operatorStore.GetOperatorsPrefix()
}

func (s *storage) GetRecipientData(r basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error) {
	return s.recipientStore.GetRecipientData(r, owner)
}

func (s *storage) GetRecipientDataMany(r basedb.Reader, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	return s.recipientStore.GetRecipientDataMany(r, owners)
}

func (s *storage) SaveRecipientData(rw basedb.ReadWriter, recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	return s.recipientStore.SaveRecipientData(rw, recipientData)
}

func (s *storage) DeleteRecipientData(rw basedb.ReadWriter, owner common.Address) error {
	return s.recipientStore.DeleteRecipientData(rw, owner)
}

func (s *storage) GetNextNonce(r basedb.Reader, owner common.Address) (registrystorage.Nonce, error) {
	return s.recipientStore.GetNextNonce(r, owner)
}

func (s *storage) BumpNonce(rw basedb.ReadWriter, owner common.Address) error {
	return s.recipientStore.BumpNonce(rw, owner)
}

func (s *storage) GetRecipientsPrefix() []byte {
	return s.recipientStore.GetRecipientsPrefix()
}

func (s *storage) DropRegistryData() error {
	err := s.dropLastProcessedBlock()
	if err != nil {
		return errors.Wrap(err, "failed to drop last processed block")
	}
	err = s.DropShares()
	if err != nil {
		return errors.Wrap(err, "failed to drop operators")
	}
	err = s.DropOperators()
	if err != nil {
		return errors.Wrap(err, "failed to drop recipients")
	}
	err = s.DropRecipients()
	if err != nil {
		return errors.Wrap(err, "failed to drop shares")
	}
	return nil
}

// TODO: review what's not needed anymore and delete

func (s *storage) SaveLastProcessedBlock(rw basedb.ReadWriter, offset *big.Int) error {
	return s.db.Using(rw).Set(OperatorStoragePrefix, lastProcessedBlockKey, offset.Bytes())
}

func (s *storage) dropLastProcessedBlock() error {
	return s.db.DropPrefix(append(OperatorStoragePrefix, lastProcessedBlockKey...))
}

func (s *storage) DropOperators() error {
	return s.operatorStore.DropOperators()
}

func (s *storage) DropRecipients() error {
	return s.recipientStore.DropRecipients()
}

func (s *storage) DropShares() error {
	return s.shareStore.Drop()
}

// GetLastProcessedBlock returns the last processed block.
func (s *storage) GetLastProcessedBlock(r basedb.Reader) (*big.Int, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(OperatorStoragePrefix, lastProcessedBlockKey)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}

	offset := new(big.Int).SetBytes(obj.Value)
	return offset, found, nil
}

// GetPrivateKeyHash return sha256 hashed private key
func (s *storage) GetPrivateKeyHash() (string, bool, error) {
	obj, found, err := s.db.Get(OperatorStoragePrefix, []byte(hashedPrivkeyDBKey))
	if !found {
		return "", found, nil
	}
	if err != nil {
		return "", found, err
	}
	return string(obj.Value), found, nil
}

// SavePrivateKeyHash saves operator private key hash
func (s *storage) SavePrivateKeyHash(hashedKey string) error {
	return s.db.Set(OperatorStoragePrefix, []byte(hashedPrivkeyDBKey), []byte(hashedKey))
}

// GetPublicKey returns public key.
func (s *storage) GetPublicKey() (string, bool, error) {
	obj, found, err := s.db.Get(OperatorStoragePrefix, []byte(pubkeyDBKey))
	if !found {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return string(obj.Value), true, nil
}

// SavePublicKey saves operator public key.
func (s *storage) SavePublicKey(publicKey string) error {
	return s.db.Set(OperatorStoragePrefix, []byte(pubkeyDBKey), []byte(publicKey))
}

func (s *storage) GetConfig(rw basedb.ReadWriter) (*ConfigLock, bool, error) {
	obj, found, err := s.db.Using(rw).Get(OperatorStoragePrefix, configKey)
	if err != nil {
		return nil, false, fmt.Errorf("db: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	config := &ConfigLock{}
	if err := json.Unmarshal(obj.Value, &config); err != nil {
		return nil, false, fmt.Errorf("unmarshal: %w", err)
	}

	return config, true, nil
}

func (s *storage) SaveConfig(rw basedb.ReadWriter, config *ConfigLock) error {
	b, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := s.db.Using(rw).Set(OperatorStoragePrefix, configKey, b); err != nil {
		return fmt.Errorf("db: %w", err)
	}

	return nil
}

func (s *storage) DeleteConfig(rw basedb.ReadWriter) error {
	return s.db.Using(rw).Delete(OperatorStoragePrefix, configKey)
}
