package ekm

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
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
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/go-bitfield"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"sync"
)

type ethKeyManagerSigner struct {
	wallet       core.Wallet
	walletLock   *sync.RWMutex
	signer       signer.ValidatorSigner
	storage      *signerStorage
	signingUtils beacon.SigningUtil
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

	beaconSigner, err := newBeaconSigner(wallet, signerStore, network)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create signer")
	}

	return &ethKeyManagerSigner{
		wallet:       wallet,
		walletLock:   &sync.RWMutex{},
		signer:       beaconSigner,
		storage:      signerStore,
		signingUtils: signingUtils,
	}, nil
}

func newBeaconSigner(wallet core.Wallet, store core.SlashingStore, network core.Network) (signer.ValidatorSigner, error) {
	slashingProtection := slashingprotection.NewNormalProtection(store)
	return signer.NewSimpleSigner(wallet, slashingProtection, network), nil
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

func (km *ethKeyManagerSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	domain, err := km.signingUtils.GetDomain(data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get domain for signing")
	}
	root, err := km.signingUtils.ComputeSigningRoot(data, domain[:])
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get root for signing")
	}
	sig, err := km.signer.SignBeaconAttestation(specAttDataToPrysmAttData(data), domain, pk)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to sign attestation")
	}
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not sign attestation")
	}

	aggregationBitfield := bitfield.NewBitlist(duty.CommitteeLength)
	aggregationBitfield.SetBitAt(duty.ValidatorCommitteeIndex, true)
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)
	return &spec.Attestation{
		AggregationBits: aggregationBitfield,
		Data:            data,
		Signature:       blsSig,
	}, root[:], nil
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

// specAttDataToPrysmAttData a simple func converting between data types
func specAttDataToPrysmAttData(data *spec.AttestationData) *eth.AttestationData {
	// TODO - adopt github.com/attestantio/go-eth2-client in eth2-key-manager
	return &eth.AttestationData{
		Slot:            types.Slot(data.Slot),
		CommitteeIndex:  types.CommitteeIndex(data.Index),
		BeaconBlockRoot: data.BeaconBlockRoot[:],
		Source: &eth.Checkpoint{
			Epoch: types.Epoch(data.Source.Epoch),
			Root:  data.Source.Root[:],
		},
		Target: &eth.Checkpoint{
			Epoch: types.Epoch(data.Target.Epoch),
			Root:  data.Target.Root[:],
		},
	}
}
