package ekm

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/eth2-key-manager/wallets"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	registry "github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
)

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
type Storage interface {
	registry.RegistryStore
	core.Storage
	core.SlashingStore

	RemoveHighestAttestation(pubKey []byte) error
	RemoveHighestProposal(pubKey []byte) error
}

type storage struct {
	db      basedb.IDb
	network beacon.Network
	lock    sync.RWMutex
}

func NewSignerStorage(db basedb.IDb, network beacon.Network) Storage {
	return &storage{
		db:      db,
		network: network,
		lock:    sync.RWMutex{},
	}
}

func (s *storage) CleanRegistryData() error {
	fmt.Println(fmt.Sprintf("RemoveAllByCollection %s", string(s.network.Network)+prefix))
	return s.db.RemoveAllByCollection(s.objPrefix(prefix))
}

func (s *storage) objPrefix(obj string) []byte {
	return []byte(string(s.network.Network) + obj)
}

// Name returns storage name.
func (s *storage) Name() string {
	return "SSV Storage"
}

// Network returns the network storage is related to.
func (s *storage) Network() core.Network {
	return s.network.Network
}

// SaveWallet stores the given wallet.
func (s *storage) SaveWallet(wallet core.Wallet) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := json.Marshal(wallet)
	if err != nil {
		return errors.Wrap(err, "failed to marshal wallet")
	}

	return s.db.Set(s.objPrefix(walletPrefix), []byte(walletPath), data)
}

// OpenWallet returns nil,err if no wallet was found
func (s *storage) OpenWallet() (core.Wallet, error) {
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
func (s *storage) ListAccounts() ([]core.ValidatorAccount, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	ret := make([]core.ValidatorAccount, 0)

	err := s.db.GetAll(s.objPrefix(accountsPrefix), func(i int, obj basedb.Obj) error {
		acc, err := s.decodeAccount(obj.Value)
		if err != nil {
			return errors.Wrap(err, "failed to list accounts")
		}
		ret = append(ret, acc)
		return nil
	})

	return ret, err
}

// SaveAccount saves the given account
func (s *storage) SaveAccount(account core.ValidatorAccount) error {
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
func (s *storage) DeleteAccount(accountID uuid.UUID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	key := fmt.Sprintf(accountsPath, accountID.String())
	return s.db.Delete(s.objPrefix(accountsPrefix), []byte(key))
}

// OpenAccount returns nil,nil if no account was found
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
		return nil, errors.Wrap(err, "failed to open account")
	}
	return s.decodeAccount(obj.Value)
}

func (s *storage) decodeAccount(byts []byte) (core.ValidatorAccount, error) {
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
func (s *storage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {

}

func (s *storage) SaveHighestAttestation(pubKey []byte, attestation *eth.AttestationData) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := attestation.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	return s.db.Set(s.objPrefix(highestAttPrefix), pubKey, data)
}

func (s *storage) RetrieveHighestAttestation(pubKey []byte) *eth.AttestationData {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestAttPrefix), pubKey)
	if err != nil {
		return nil
	}
	if !found {
		return nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil
	}

	// decode
	ret := &eth.AttestationData{}
	if err := ret.UnmarshalSSZ(obj.Value); err != nil {
		return nil
	}
	return ret
}

func (s *storage) RemoveHighestAttestation(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestAttPrefix), pubKey)
}

func (s *storage) SaveHighestProposal(pubKey []byte, block *eth.BeaconBlock) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	data, err := block.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "failed to marshal beacon block")
	}

	return s.db.Set(s.objPrefix(highestProposalPrefix), pubKey, data)
}

func (s *storage) RetrieveHighestProposal(pubKey []byte) *eth.BeaconBlock {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// get wallet bytes
	obj, found, err := s.db.Get(s.objPrefix(highestProposalPrefix), pubKey)
	if err != nil {
		return nil
	}
	if !found {
		return nil
	}
	if obj.Value == nil || len(obj.Value) == 0 {
		return nil
	}

	// decode
	ret := &eth.BeaconBlock{}
	if err := ret.UnmarshalSSZ(obj.Value); err != nil {
		return nil
	}
	return ret
}

func (s *storage) RemoveHighestProposal(pubKey []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.objPrefix(highestProposalPrefix), pubKey)
}
