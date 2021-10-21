package goclient

import (
	"encoding/json"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"sync"
)

const (
	prefix     = "signer_data-"
	walletPath = "wallet"
)

func storagePrefix(network core.Network) []byte {
	return []byte(prefix + network)
}

type signerStorage struct {
	db      basedb.IDb
	network core.Network
	lock    sync.RWMutex
	prefix  []byte
}

func newSignerStorage(db basedb.IDb, network core.Network) *signerStorage {
	return &signerStorage{
		db:      db,
		network: network,
		lock:    sync.RWMutex{},
		prefix:  storagePrefix(network),
	}
}

// Name returns storage name.
func (s *signerStorage) Name() string {
	return "SSV Storage"
}

// Network returns the network storage is related to.
func (s *signerStorage) Network() core.Network {
	return s.network
}

// SaveWallet stores the given wallet.
func (s *signerStorage) SaveWallet(wallet core.Wallet) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(wallet)
	if err != nil {
		return errors.Wrap(err, "failed to marshal wallet")
	}

	return s.db.Set(s.prefix, []byte(walletPath), data)
}

// OpenWallet returns nil,err if no wallet was found
func (s *signerStorage) OpenWallet() (core.Wallet, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// get wallet bytes
	obj, found, err := s.db.Get(s.prefix, []byte(walletPath))
	if !found {
		return nil, errors.New("could not find wallet")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to open wallet")
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil, errors.Wrap(err, "failed to open wallet")
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
	s.lock.RLock()
	defer s.lock.RUnlock()
	return nil, nil
}

// SaveAccount saves the given account
func (s *signerStorage) SaveAccount(account core.ValidatorAccount) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return nil
}

// DeleteAccount deletes account by uuid
func (s *signerStorage) DeleteAccount(accountID uuid.UUID) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return nil
}

// OpenAccount returns nil,nil if no account was found
func (s *signerStorage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return nil, nil
}

// SetEncryptor sets the given encryptor to the wallet.
func (s *signerStorage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {

}

func (s *signerStorage) SaveHighestAttestation(pubKey []byte, attestation *eth.AttestationData) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return nil
}

func (s *signerStorage) RetrieveHighestAttestation(pubKey []byte) *eth.AttestationData {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return nil
}

func (s *signerStorage) SaveHighestProposal(pubKey []byte, block *eth.BeaconBlock) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return nil
}

func (s *signerStorage) RetrieveHighestProposal(pubKey []byte) *eth.BeaconBlock {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return nil
}
