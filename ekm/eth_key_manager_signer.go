package ekm

import (
	"crypto/rsa"
	"encoding/hex"
	"github.com/prysmaticlabs/go-bitfield"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	eth2keymanager "github.com/bloxapp/eth2-key-manager"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/signer"
	slashingprotection "github.com/bloxapp/eth2-key-manager/slashing_protection"
	"github.com/bloxapp/eth2-key-manager/wallets"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
)

type ethKeyManagerSigner struct {
	wallet       core.Wallet
	walletLock   *sync.RWMutex
	signer       signer.ValidatorSigner
	storage      *signerStorage
	signingUtils beacon.SigningUtil
	domain       spectypes.DomainType
}

// NewETHKeyManagerSigner returns a new instance of ethKeyManagerSigner
func NewETHKeyManagerSigner(db basedb.IDb, signingUtils beaconprotocol.SigningUtil, network beaconprotocol.Network, domain spectypes.DomainType) (spectypes.KeyManager, error) {
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
		domain:       domain,
	}, nil
}

func newBeaconSigner(wallet core.Wallet, store core.SlashingStore, network beaconprotocol.Network) (signer.ValidatorSigner, error) {
	slashingProtection := slashingprotection.NewNormalProtection(store)
	return signer.NewSimpleSigner(wallet, slashingProtection, network.Network), nil
}

func (km *ethKeyManagerSigner) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	domain, err := km.signingUtils.GetDomain(data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not get domain for signing")
	}
	root, err := km.signingUtils.ComputeSigningRoot(data, domain[:])
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not compute signing root")
	}
	sig, err := km.signer.SignBeaconAttestation(specAttDataToPrysmAttData(data), domain, pk)
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

func (km *ethKeyManagerSigner) IsAttestationSlashable(data *spec.AttestationData) error {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignRandaoReveal(epoch spec.Epoch, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) IsBeaconBlockSlashable(block *altair.BeaconBlock) error {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignBeaconBlock(block *altair.BeaconBlock, duty *spectypes.Duty, pk []byte) (*altair.SignedBeaconBlock, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignSlotWithSelectionProof(slot spec.Slot, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignAggregateAndProof(msg *spec.AggregateAndProof, duty *spectypes.Duty, pk []byte) (*spec.SignedAggregateAndProof, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignSyncCommitteeBlockRoot(slot spec.Slot, root spec.Root, validatorIndex spec.ValidatorIndex, pk []byte) (*altair.SyncCommitteeMessage, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignContributionProof(slot spec.Slot, index uint64, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignContribution(contribution *altair.ContributionAndProof, pk []byte) (*altair.SignedContributionAndProof, []byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) Decrypt(pk *rsa.PublicKey, cipher []byte) ([]byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) Encrypt(pk *rsa.PublicKey, data []byte) ([]byte, error) {
	panic("implement me")
}

func (km *ethKeyManagerSigner) SignRoot(data spectypes.Root, sigType spectypes.SignatureType, pk []byte) (spectypes.Signature, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	account, err := km.wallet.AccountByPublicKey(hex.EncodeToString(pk))
	if err != nil {
		return nil, errors.Wrap(err, "could not get signing account")
	}

	root, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(km.domain, sigType))
	if err != nil {
		return nil, errors.Wrap(err, "could not compute signing root")
	}

	sig, err := account.ValidationKeySign(root)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign message")
	}

	return sig, nil
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

func (km *ethKeyManagerSigner) RemoveShare(pubKey string) error {
	km.walletLock.Lock()
	defer km.walletLock.Unlock()

	acc, err := km.wallet.AccountByPublicKey(pubKey)
	if err != nil && err.Error() != "account not found" {
		return errors.Wrap(err, "could not check share existence")
	}
	if acc != nil {
		if err := km.wallet.DeleteAccountByPublicKey(pubKey); err != nil {
			return errors.Wrap(err, "could not delete share")
		}
	}
	return nil
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
