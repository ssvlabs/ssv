package ekm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/google/uuid"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/encryptor"
	"github.com/ssvlabs/eth2-key-manager/wallets"
	"github.com/ssvlabs/eth2-key-manager/wallets/hd"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registry "github.com/ssvlabs/ssv/protocol/v2/blockchain/eth1"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// signer_storage.go provides a concrete implementation of Storage (backed by
// basedb.Database) to store wallet and slashing data. It also supports optional
// encryption of the stored data via SetEncryptionKey.

const (
	prefix                = "signer_data-"
	walletPrefix          = prefix + "wallet-"
	walletPath            = "wallet"
	accountsPrefix        = prefix + "accounts-"
	accountsPath          = "accounts_%s"
	highestAttPrefix      = prefix + "highest_att-"
	highestProposalPrefix = prefix + "highest_prop-"
)

// Storage represents the interface for ssv node storage
// TODO: review if we need all of them
type Storage interface {
	registry.RegistryStore
	core.Storage
	core.SlashingStore

	RemoveHighestAttestation(pubKey []byte) error
	RemoveHighestProposal(pubKey []byte) error
	SetEncryptionKey(hexKey string) error
	ListAccountsTxn(r basedb.Reader) ([]core.ValidatorAccount, error)
	SaveAccountTxn(rw basedb.ReadWriter, account core.ValidatorAccount) error

	BeaconNetwork() beacon.BeaconNetwork
}

// storage is an internal struct implementing the Storage interface. It uses
// a basedb.Database for persistence, locks for concurrency protection, and
// an optional encryption key to secure stored wallet/account data.
//
// The object keys are prefixed by network name and entity name
// (walletPrefix, highestAttPrefix, etc.).
type storage struct {
	db            basedb.Database
	network       beacon.BeaconNetwork
	encryptionKey []byte
	logger        *zap.Logger // struct logger is used because core.Storage does not support passing a logger
	lock          sync.RWMutex
}

func NewSignerStorage(db basedb.Database, network beacon.BeaconNetwork, logger *zap.Logger) Storage {
	return &storage{
		db:      db,
		network: network,
		logger:  logger.Named(logging.NameSignerStorage).Named(prefix + "storage"),
		lock:    sync.RWMutex{},
	}
}

// SetEncryptionKey sets the hex-encoded encryption key used
// to encrypt/decrypt account data. If no key is set (empty),
// the data is stored in plaintext.
func (s *storage) SetEncryptionKey(hexKey string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Decode hexadecimal string into byte array
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return errors.New("the key must be a valid hexadecimal string")
	}

	// Set the encryption key
	s.encryptionKey = keyBytes
	return nil
}

func (s *storage) DropRegistryData() error {
	return s.db.DropPrefix(s.objPrefix(accountsPrefix))
}

func (s *storage) objPrefix(obj string) []byte {
	return []byte(string(s.network.GetBeaconNetwork()) + obj)
}

// Name returns storage name.
func (s *storage) Name() string {
	return "SSV Storage"
}

// Network returns the network storage is related to.
func (s *storage) Network() core.Network {
	return core.Network(s.network.GetBeaconNetwork())
}

// SaveWallet stores the given wallet.
func (s *storage) SaveWallet(wallet core.Wallet) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(wallet)
	if err != nil {
		return fmt.Errorf("marshal wallet: %w", err)
	}

	return s.db.Set(s.objPrefix(walletPrefix), []byte(walletPath), data)
}

// OpenWallet returns the main HD wallet if present, otherwise an error.
// The returned wallet is configured with this storage as its backend.
func (s *storage) OpenWallet() (core.Wallet, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(walletPrefix), []byte(walletPath))
	if !found {
		return nil, errors.New("could not find wallet")
	}
	if err != nil {
		return nil, fmt.Errorf("open wallet: %w", err)
	}
	if len(obj.Value) == 0 {
		return nil, errors.New("failed to open wallet")
	}
	// decode
	var ret *hd.Wallet
	if err := json.Unmarshal(obj.Value, &ret); err != nil {
		return nil, fmt.Errorf("unmarshal HD Wallet object: %w", err)
	}
	ret.SetContext(&core.WalletContext{Storage: s})
	return ret, nil
}

// ListAccounts returns all validator accounts known to the wallet storage.
// If no accounts are found, returns an empty slice with no error.
func (s *storage) ListAccounts() ([]core.ValidatorAccount, error) {
	return s.ListAccountsTxn(nil)
}

// ListAccountsTxn returns an empty array for no accounts.
func (s *storage) ListAccountsTxn(r basedb.Reader) ([]core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	ret := make([]core.ValidatorAccount, 0)

	err := s.db.UsingReader(r).GetAll(s.objPrefix(accountsPrefix), func(i int, obj basedb.Obj) error {
		value, err := s.decryptData(obj.Value)
		if err != nil {
			return fmt.Errorf("decrypt accounts: %w", err)
		}
		acc, err := s.decodeAccount(value)
		if err != nil {
			return fmt.Errorf("list accounts: %w", err)
		}
		ret = append(ret, acc)
		return nil
	})

	return ret, err
}

func (s *storage) SaveAccountTxn(rw basedb.ReadWriter, account core.ValidatorAccount) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("marshal account: %w", err)
	}

	key := fmt.Sprintf(accountsPath, account.ID().String())

	encryptedValue, err := s.encryptData(data)
	if err != nil {
		return err
	}

	return s.db.Using(rw).Set(s.objPrefix(accountsPrefix), []byte(key), encryptedValue)
}

// SaveAccount saves the given account.
func (s *storage) SaveAccount(account core.ValidatorAccount) error {
	return s.SaveAccountTxn(nil, account)
}

// DeleteAccount deletes account by uuid.
func (s *storage) DeleteAccount(accountID uuid.UUID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key := fmt.Sprintf(accountsPath, accountID.String())
	return s.db.Delete(s.objPrefix(accountsPrefix), []byte(key))
}

// OpenAccount returns nil,nil if no account was found.
func (s *storage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	key := fmt.Sprintf(accountsPath, accountID.String())

	// get account bytes
	obj, found, err := s.db.Get(s.objPrefix(accountsPrefix), []byte(key))
	if !found {
		return nil, errors.New("account not found")
	}
	if err != nil {
		return nil, fmt.Errorf("open account: %w", err)
	}
	decryptedData, err := s.decryptData(obj.Value)
	if err != nil {
		return nil, fmt.Errorf("decrypt stored wallet (wrong password?): %w", err)
	}
	return s.decodeAccount(decryptedData)
}

func (s *storage) decodeAccount(byts []byte) (core.ValidatorAccount, error) {
	if len(byts) == 0 {
		return nil, errors.New("bytes are empty")
	}

	// decode
	var ret *wallets.HDAccount
	if err := json.Unmarshal(byts, &ret); err != nil {
		return nil, fmt.Errorf("unmarshal HD account object: %w", err)
	}
	ret.SetContext(&core.WalletContext{Storage: s})

	return ret, nil
}

// SetEncryptor sets the given encryptor to the wallet. It's a no-op to match the core.AccountStorage interface.
func (s *storage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {}

// SaveHighestAttestation persists the highest known AttestationData for the
// given public key. Used to ensure we do not sign slashable attestations.
func (s *storage) SaveHighestAttestation(pubKey []byte, attestation *phase0.AttestationData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if pubKey == nil {
		return errors.New("pubKey must not be nil")
	}

	if attestation == nil {
		return errors.New("attestation data could not be nil")
	}

	data, err := attestation.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal attestation: %w", err)
	}

	return s.db.Set(s.objPrefix(highestAttPrefix), pubKey, data)
}

// RetrieveHighestAttestation fetches the stored highest attestation data.
// Returns attestation data, whether it was found, and error.
func (s *storage) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return nil, false, errors.New("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestAttPrefix), pubKey)
	if err != nil {
		return nil, found, fmt.Errorf("could not get highest attestation from db: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	if len(obj.Value) == 0 {
		return nil, found, fmt.Errorf("highest attestation value is empty: %w", err)
	}

	// decode
	ret := &phase0.AttestationData{}
	if err := ret.UnmarshalSSZ(obj.Value); err != nil {
		return nil, found, fmt.Errorf("could not unmarshal attestation data: %w", err)
	}
	return ret, found, nil
}

func (s *storage) RemoveHighestAttestation(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestAttPrefix), pubKey)
}

// SaveHighestProposal stores the highest known proposal slot for the given
// public key to prevent slashable block proposals.
func (s *storage) SaveHighestProposal(pubKey []byte, slot phase0.Slot) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if pubKey == nil {
		return errors.New("pubKey must not be nil")
	}

	if slot == 0 {
		return errors.New("invalid proposal slot, slot could not be 0")
	}

	var data []byte
	data = ssz.MarshalUint64(data, uint64(slot))

	return s.db.Set(s.objPrefix(highestProposalPrefix), pubKey, data)
}

// RetrieveHighestProposal loads the highest proposal slot from storage.
// Returns attestation data, whether it was found, and error.
func (s *storage) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if pubKey == nil {
		return 0, false, errors.New("public key could not be nil")
	}

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestProposalPrefix), pubKey)
	if err != nil {
		return 0, found, fmt.Errorf("could not get highest proposal from db: %w", err)
	}
	if !found {
		return 0, found, nil
	}
	if len(obj.Value) == 0 {
		return 0, found, fmt.Errorf("highest proposal value is empty: %w", err)
	}

	// decode
	slot := phase0.Slot(ssz.UnmarshallUint64(obj.Value))
	return slot, found, nil
}

func (s *storage) RemoveHighestProposal(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestProposalPrefix), pubKey)
}

func (s *storage) decryptData(objectValue []byte) ([]byte, error) {
	if len(s.encryptionKey) == 0 {
		return objectValue, nil
	}

	decryptedData, err := s.decrypt(objectValue)
	if err != nil {
		return nil, fmt.Errorf("decrypt wallet: %w", err)
	}

	return decryptedData, nil
}

func (s *storage) encryptData(objectValue []byte) ([]byte, error) {
	if len(s.encryptionKey) == 0 {
		return objectValue, nil
	}

	encryptedData, err := s.encrypt(objectValue)
	if err != nil {
		return nil, fmt.Errorf("encrypt wallet: %w", err)
	}

	return encryptedData, nil
}

func (s *storage) encrypt(plaintext []byte) ([]byte, error) {
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
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *storage) decrypt(nonceCipherText []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(nonceCipherText) < nonceSize {
		return nil, errors.New("malformed ciphertext")
	}

	nonce, ciphertext := nonceCipherText[:nonceSize], nonceCipherText[nonceSize:]
	// #nosec G407 false positive: https://github.com/securego/gosec/issues/1211
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func (s *storage) BeaconNetwork() beacon.BeaconNetwork {
	return s.network
}
