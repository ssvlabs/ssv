package ekm

import (
	"encoding/hex"
	eth2keymanager "github.com/bloxapp/eth2-key-manager"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/signer"
	slashingprotection "github.com/bloxapp/eth2-key-manager/slashing_protection"
	"github.com/bloxapp/eth2-key-manager/wallets"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"sync"
)

type ethKeyManagerSigner struct {
	wallet             core.Wallet
	walletLock         *sync.RWMutex
	signer             signer.ValidatorSigner
	slashingProtection core.SlashingProtector
	storage            *signerStorage
	signingUtils       beacon.SigningUtil
}

// NewETHKeyManagerSigner returns a new instance of ethKeyManagerSigner
func NewETHKeyManagerSigner(db basedb.IDb, signingUtils beacon.SigningUtil, network core.Network) (beacon.KeyManager, error) {
	signerStore := newSignerStorage(db, network)
	options := &eth2keymanager.KeyVaultOptions{}
	options.SetStorage(signerStore)
	options.SetWalletType(core.NDWallet)

	wallet, err := signerStore.OpenWallet()
	if err != nil && err.Error() != "could not find wallet" {
		return nil, err
	}
	if wallet == nil {
		vault, err := eth2keymanager.NewKeyVault(options)
		if err != nil {
			return nil, err
		}
		wallet, err = vault.Wallet()
		if err != nil {
			return nil, err
		}
	}

	slashingProtection := slashingprotection.NewNormalProtection(signerStore)
	beaconSigner := signer.NewSimpleSigner(wallet, slashingProtection, network)

	return &ethKeyManagerSigner{
		wallet:             wallet,
		walletLock:         &sync.RWMutex{},
		signer:             beaconSigner,
		slashingProtection: slashingProtection,
		storage:            signerStore,
		signingUtils:       signingUtils,
	}, nil
}

func (km *ethKeyManagerSigner) AddShare(shareKey *bls.SecretKey) error {
	km.walletLock.Lock()
	defer km.walletLock.Unlock()

	acc, err := km.wallet.AccountByPublicKey(shareKey.GetPublicKey().SerializeToHexStr())
	if err != nil && err.Error() != "account not found" {
		return errors.Wrap(err, "could not check share existence")
	}
	if acc == nil {
		if err := km.storage.SaveHighestAttestation(shareKey.GetPublicKey().Serialize(), zeroSlotAttestation); err != nil {
			return errors.Wrap(err, "could not save zero highest attestation")
		}
		if err := km.saveShare(shareKey); err != nil {
			return errors.Wrap(err, "could not save share")
		}
	}
	return nil
}

func (km *ethKeyManagerSigner) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	root, err := message.SigningRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not get message signing root")
	}

	account, err := km.wallet.AccountByPublicKey(hex.EncodeToString(pk))
	if err != nil {
		return nil, errors.Wrap(err, "could not get signing account")
	}

	sig, err := account.ValidationKeySign(root)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign message")
	}

	return sig, nil
}

func (km *ethKeyManagerSigner) saveShare(shareKey *bls.SecretKey) error {
	key, err := core.NewHDKeyFromPrivateKey(shareKey.Serialize(), "")
	if err != nil {
		return errors.Wrap(err, "could not generate HDKey")
	}
	account := wallets.NewValidatorAccount("", key, nil, "", nil)
	if err := km.wallet.AddValidatorAccount(account); err != nil {
		return errors.Wrap(err, "could not save new account")
	}
	return nil
}

// zeroSlotAttestation is a place holder attestation data representing all zero values
var zeroSlotAttestation = &eth.AttestationData{
	Slot:            0,
	CommitteeIndex:  0,
	BeaconBlockRoot: make([]byte, 32),
	Source: &eth.Checkpoint{
		Epoch: 0,
		Root:  make([]byte, 32),
	},
	Target: &eth.Checkpoint{
		Epoch: 0,
		Root:  make([]byte, 32),
	},
}
