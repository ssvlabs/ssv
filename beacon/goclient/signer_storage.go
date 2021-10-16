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

}

// OpenWallet returns nil,err if no wallet was found
func (s *signerStorage) OpenWallet() (core.Wallet, error) {

}

// ListAccounts returns an empty array for no accounts
func (s *signerStorage) ListAccounts() ([]core.ValidatorAccount, error) {

}

// SaveAccount saves the given account
func (s *signerStorage) SaveAccount(account core.ValidatorAccount) error {

}

// DeleteAccount deletes account by uuid
func (s *signerStorage) DeleteAccount(accountID uuid.UUID) error {

}

// OpenAccount returns nil,nil if no account was found
func (s *signerStorage) OpenAccount(accountID uuid.UUID) (core.ValidatorAccount, error) {

}

// SetEncryptor sets the given encryptor to the wallet.
func (s *signerStorage) SetEncryptor(encryptor encryptor.Encryptor, password []byte) {

}

func (s *signerStorage) SaveHighestAttestation(pubKey []byte, attestation *eth.AttestationData) error {

}

func (s *signerStorage) RetrieveHighestAttestation(pubKey []byte) *eth.AttestationData {

}

func (s *signerStorage) SaveHighestProposal(pubKey []byte, block *eth.BeaconBlock) error {

}

func (s *signerStorage) RetrieveHighestProposal(pubKey []byte) *eth.BeaconBlock {

}
