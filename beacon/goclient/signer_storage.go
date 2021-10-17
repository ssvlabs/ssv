package goclient

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/encryptor"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/google/uuid"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
)

type signerStorage struct {
}

func newSignerStorage(DB basedb.IDb) *signerStorage {
	return &signerStorage{}
}

// SaveWallet stores the given wallet.
func (s *signerStorage) SaveWallet(wallet core.Wallet) error {
	return nil
}

// OpenWallet returns nil,err if no wallet was found
func (s *signerStorage) OpenWallet() (core.Wallet, error) {
	return nil, nil
}

// ListAccounts returns an empty array for no accounts
func (s *signerStorage) ListAccounts() ([]core.ValidatorAccount, error) {
	return nil, nil
}

// SaveAccount saves the given account
func (s *signerStorage) SaveAccount(account core.ValidatorAccount) error {
	return nil
}

// DeleteAccount deletes account by uuid
func (s *signerStorage) DeleteAccount(accountID uuid.UUID) error {
	return nil
}

// OpenAccount returns nil,nil if no account was found
func (s *signerStorage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {
	return nil, nil
}

// SetEncryptor sets the given encryptor to the wallet.
func (s *signerStorage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {

}

func (s *signerStorage) SaveHighestAttestation(pubKey []byte, attestation *eth.AttestationData) error {
	return nil
}

func (s *signerStorage) RetrieveHighestAttestation(pubKey []byte) *eth.AttestationData {
	return nil
}

func (s *signerStorage) SaveHighestProposal(pubKey []byte, block *eth.BeaconBlock) error {
	return nil
}

func (s *signerStorage) RetrieveHighestProposal(pubKey []byte) *eth.BeaconBlock {
	return nil
}
