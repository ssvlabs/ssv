package ekm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/wallets"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	walletPrefix   = prefix + "wallet-"
	walletPath     = "wallet"
	accountsPrefix = prefix + "accounts-"
	accountsPath   = "accounts_%s"
)

type SignerStorage interface {
	registry.RegistryStore
	core.Storage

	SetEncryptionKey(newKey string) error
	ListAccountsTxn(r basedb.Reader) ([]core.ValidatorAccount, error)
	SaveAccountTxn(rw basedb.ReadWriter, account core.ValidatorAccount) error
}

type signerStorage struct {
	db            basedb.Database
	encryptionKey []byte
	logger        *zap.Logger // struct logger is used because core.Storage does not support passing a logger
	network       spectypes.BeaconNetwork
	prefix        []byte
	lock          sync.RWMutex
}

func NewSignerStorage(db basedb.Database, logger *zap.Logger, network spectypes.BeaconNetwork, prefix []byte) SignerStorage {
	return &signerStorage{
		db:      db,
		logger:  logger.Named(logging.NameSignerStorage).Named(fmt.Sprintf("%sstorage", prefix)),
		network: network,
		prefix:  prefix,
		lock:    sync.RWMutex{},
	}
}

// SetEncryptionKey Add a new method to the storage type
func (s *signerStorage) SetEncryptionKey(newKey string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Decode hexadecimal string into byte array
	keyBytes, err := hex.DecodeString(newKey)
	if err != nil {
		return errors.New("the key must be a valid hexadecimal string")
	}

	// Set the encryption key
	s.encryptionKey = keyBytes
	return nil
}

func (s *signerStorage) DropRegistryData() error {
	return s.db.DropPrefix(s.objPrefix(accountsPrefix))
}

func (s *signerStorage) objPrefix(obj string) []byte {
	return append(s.prefix, []byte(obj)...)
}

// Name returns storage name.
func (s *signerStorage) Name() string {
	return "SSV Storage"
}

// Network returns the network storage is related to.
func (s *signerStorage) Network() core.Network {
	return core.Network(s.network)
}

// SaveWallet stores the given wallet.
func (s *signerStorage) SaveWallet(wallet core.Wallet) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(wallet)
	if err != nil {
		return errors.Wrap(err, "failed to marshal wallet")
	}

	return s.db.Set(s.objPrefix(walletPrefix), []byte(walletPath), data)
}

// OpenWallet returns nil,err if no wallet was found
func (s *signerStorage) OpenWallet() (core.Wallet, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(walletPrefix), []byte(walletPath))
	if !found {
		return nil, errors.New("could not find wallet")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to open wallet")
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil, errors.New("failed to open wallet")
	}
	// decode
	var ret *hd.Wallet
	if err := json.Unmarshal(obj.Value, &ret); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal HD Wallet object")
	}
	ret.SetContext(&core.WalletContext{Storage: s})
	return ret, nil
}

// ListAccounts returns an empty array for no accounts
func (s *signerStorage) ListAccounts() ([]core.ValidatorAccount, error) {
	return s.ListAccountsTxn(nil)
}

// ListAccountsTxn returns an empty array for no accounts
func (s *signerStorage) ListAccountsTxn(r basedb.Reader) ([]core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	ret := make([]core.ValidatorAccount, 0)

	err := s.db.UsingReader(r).GetAll(s.objPrefix(accountsPrefix), func(i int, obj basedb.Obj) error {
		value, err := s.decryptData(obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt accounts")
		}
		acc, err := s.decodeAccount(value)
		if err != nil {
			return errors.Wrap(err, "failed to list accounts")
		}
		ret = append(ret, acc)
		return nil
	})

	return ret, err
}

func (s *signerStorage) SaveAccountTxn(rw basedb.ReadWriter, account core.ValidatorAccount) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(account)
	if err != nil {
		return errors.Wrap(err, "failed to marshal account")
	}

	key := fmt.Sprintf(accountsPath, account.ID().String())

	encryptedValue, err := s.encryptData(data)
	if err != nil {
		return err
	}
	return s.db.Using(rw).Set(s.objPrefix(accountsPrefix), []byte(key), encryptedValue)
}

// SaveAccount saves the given account
func (s *signerStorage) SaveAccount(account core.ValidatorAccount) error {
	return s.SaveAccountTxn(nil, account)
}

// DeleteAccount deletes account by uuid
func (s *signerStorage) DeleteAccount(accountID uuid.UUID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key := fmt.Sprintf(accountsPath, accountID.String())
	return s.db.Delete(s.objPrefix(accountsPrefix), []byte(key))
}

var ErrCantDecrypt = errors.New("can't decrypt stored wallet, wrong password?")

// OpenAccount returns nil,nil if no account was found
func (s *signerStorage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	key := fmt.Sprintf(accountsPath, accountID.String())

	// get account bytes
	obj, found, err := s.db.Get(s.objPrefix(accountsPrefix), []byte(key))
	if !found {
		return nil, errors.New("account not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to open account")
	}
	decryptedData, err := s.decryptData(obj.Value)
	if err != nil {
		return nil, errors.Wrap(ErrCantDecrypt, err.Error())
	}
	return s.decodeAccount(decryptedData)
}

func (s *signerStorage) decodeAccount(byts []byte) (core.ValidatorAccount, error) {
	if len(byts) == 0 {
		return nil, errors.New("bytes are empty")
	}

	// decode
	var ret *wallets.HDAccount
	if err := json.Unmarshal(byts, &ret); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal HD account object")
	}
	ret.SetContext(&core.WalletContext{Storage: s})

	return ret, nil
}

// SetEncryptor sets the given encryptor to the wallet.
func (s *signerStorage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {

}

func (s *signerStorage) decryptData(objectValue []byte) ([]byte, error) {
	if s.encryptionKey == nil || len(s.encryptionKey) == 0 {
		return objectValue, nil
	}

	decryptedData, err := s.decrypt(objectValue)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decrypt wallet")
	}

	return decryptedData, nil
}

func (s *signerStorage) encryptData(objectValue []byte) ([]byte, error) {
	if s.encryptionKey == nil || len(s.encryptionKey) == 0 {
		return objectValue, nil
	}

	encryptedData, err := s.encrypt(objectValue)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encrypt wallet")
	}

	return encryptedData, nil
}

func (s *signerStorage) encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

func (s *signerStorage) decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("malformed ciphertext")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
