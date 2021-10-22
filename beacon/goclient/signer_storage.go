package goclient

import (
	"encoding/json"
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/wallets"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"sync"
)

const (
	prefix         = "signer_data-"
	walletPrefix   = prefix + "wallet-"
	walletPath     = "wallet"
	accountsPrefix = prefix + "accounts-"
	accountsPath   = "accounts_%s"
)

type signerStorage struct {
	db      basedb.IDb
	network core.Network
	lock    sync.RWMutex
}

func newSignerStorage(db basedb.IDb, network core.Network) *signerStorage {
	return &signerStorage{
		db:      db,
		network: network,
		lock:    sync.RWMutex{},
	}
}

func (s *signerStorage) objPrefix(obj string) []byte {
	return []byte(string(s.network) + obj)
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
	s.lock.RLock()
	defer s.lock.RUnlock()

	objs, err := s.db.GetAllByCollection(s.objPrefix(accountsPrefix))
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get val share")
	}

	ret := make([]core.ValidatorAccount, 0)
	for _, obj := range objs {
		acc, err := s.decodeAccount(obj.Value)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list accounts")
		}

		ret = append(ret, acc)
	}

	return ret, nil
}

// SaveAccount saves the given account
func (s *signerStorage) SaveAccount(account core.ValidatorAccount) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(account)
	if err != nil {
		return errors.Wrap(err, "failed to marshal account")
	}

	key := fmt.Sprintf(accountsPath, account.ID().String())

	return s.db.Set(s.objPrefix(accountsPrefix), []byte(key), data)
}

// DeleteAccount deletes account by uuid
func (s *signerStorage) DeleteAccount(accountID uuid.UUID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key := fmt.Sprintf(accountsPath, accountID.String())
	s.db.Set(s.objPrefix(accountsPrefix), []byte(key), nil)

	return nil
}

// OpenAccount returns nil,nil if no account was found
func (s *signerStorage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	key := fmt.Sprintf(accountsPath, accountID.String())

	// get account bytes
	obj, found, err := s.db.Get(s.objPrefix(accountsPrefix), []byte(key))
	if !found {
		return nil, errors.New("could not find account")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to open account")
	}
	return s.decodeAccount(obj.Value)
}

func (s *signerStorage) decodeAccount(byts []byte) (core.ValidatorAccount, error) {
	if byts == nil || len(byts) == 0 {
		return nil, errors.New("failed to open wallet")
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
