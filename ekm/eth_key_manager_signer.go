package ekm

import (
	"encoding/hex"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	eth2keymanager "github.com/bloxapp/eth2-key-manager"
	"github.com/bloxapp/eth2-key-manager/core"
	slashingprotection "github.com/bloxapp/eth2-key-manager/slashing_protection"
	"github.com/bloxapp/eth2-key-manager/wallets"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	prysmtypes "github.com/prysmaticlabs/eth2-types"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
)

// minimal att&block epoch/slot distance to protect slashing
var minimalAttSlashingProtectionEpochDistance = prysmtypes.Epoch(0)
var minimalBlockSlashingProtectionSlotDistance = prysmtypes.Slot(0)

type ethKeyManagerSigner struct {
	wallet            core.Wallet
	walletLock        *sync.RWMutex
	storage           Storage
	signingUtils      beacon.Beacon
	domain            spectypes.DomainType
	slashingProtector core.SlashingProtector
}

// NewETHKeyManagerSigner returns a new instance of ethKeyManagerSigner
func NewETHKeyManagerSigner(db basedb.IDb, signingUtils beaconprotocol.Beacon, network beaconprotocol.Network, domain spectypes.DomainType) (spectypes.KeyManager, error) {
	signerStore := NewSignerStorage(db, network)
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

	slashingProtector := newSlashingProtection(signerStore)

	return &ethKeyManagerSigner{
		wallet:            wallet,
		walletLock:        &sync.RWMutex{},
		storage:           signerStore,
		signingUtils:      signingUtils,
		domain:            domain,
		slashingProtector: slashingProtector,
	}, nil
}

func newSlashingProtection(store core.SlashingStore) core.SlashingProtector {
	return slashingprotection.NewNormalProtection(store)
}

func (km *ethKeyManagerSigner) SignBeaconObject(obj ssz.HashRoot, domain spec.Domain, pk []byte, domainType spec.DomainType) (spectypes.Signature, []byte, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	account, err := km.wallet.AccountByPublicKey(hex.EncodeToString(pk))
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not get signing account")
	}
	if account == nil {
		return nil, nil, errors.New("pk not found")
	}

	r, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not compute signing root")
	}

	if err := km.slashingProtection(pk, obj, domainType); err != nil {
		return nil, nil, err
	}

	sig, err := account.ValidationKeySign(r[:])
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not sign message")
	}
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)

	return sig, r[:], nil
}

func (km *ethKeyManagerSigner) slashingProtection(pk []byte, obj ssz.HashRoot, domainType spec.DomainType) error {
	switch domainType {
	case spectypes.DomainAttester:
		data, ok := obj.(*spec.AttestationData)
		if !ok {
			return errors.New("could not convert obj to attestation data")
		}
		if err := km.IsAttestationSlashable(pk, data); err != nil {
			return err
		}
		if err := km.slashingProtector.UpdateHighestAttestation(pk, specAttDataToPrysmAttData(data)); err != nil {
			return err
		}

	case spectypes.DomainProposer:
		data, ok := obj.(*bellatrix.BeaconBlock)
		if !ok {
			return errors.New("could not convert obj to beacon block")
		}
		if err := km.IsBeaconBlockSlashable(pk, data); err != nil {
			return err
		}
		if err := km.slashingProtector.UpdateHighestProposal(pk, specBeaconBlockToPrysmBlock(data)); err != nil {
			return err
		}
	}
	return nil
}

func (km *ethKeyManagerSigner) IsAttestationSlashable(pk []byte, data *spec.AttestationData) error {
	prysmAttData := specAttDataToPrysmAttData(data)
	if val, err := km.slashingProtector.IsSlashableAttestation(pk, prysmAttData); err != nil || val != nil {
		if err != nil {
			return err
		}
		return errors.Errorf("slashable attestation (%s), not signing", val.Status)
	}
	return nil
}

func (km *ethKeyManagerSigner) IsBeaconBlockSlashable(pk []byte, block *bellatrix.BeaconBlock) error {
	status, err := km.slashingProtector.IsSlashableProposal(pk, specBeaconBlockToPrysmBlock(block))
	if err != nil {
		return err
	}
	if status.Status != core.ValidProposal {
		return errors.Errorf("slashable proposal (%s), not signing", status.Status)
	}

	return nil
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
		if err := km.saveMinimalSlashingProtection(shareKey.GetPublicKey().Serialize()); err != nil {
			return errors.Wrap(err, "could not save minimal slashing protection")
		}
		if err := km.saveShare(shareKey); err != nil {
			return errors.Wrap(err, "could not save share")
		}
	}

	return nil
}

func (km *ethKeyManagerSigner) saveMinimalSlashingProtection(pk []byte) error {
	currentSlot := km.storage.Network().EstimatedCurrentSlot()
	currentEpoch := km.storage.Network().EstimatedEpochAtSlot(currentSlot)
	highestTarget := currentEpoch + minimalAttSlashingProtectionEpochDistance
	highestSource := highestTarget - 1
	highestProposal := currentSlot + minimalBlockSlashingProtectionSlotDistance

	minAttData := minimalAttProtectionData(highestSource, highestTarget)
	minBlockData := minimalBlockProtectionData(highestProposal)

	if err := km.storage.SaveHighestAttestation(pk, minAttData); err != nil {
		return errors.Wrapf(err, "could not save minimal highest attestation for %s", string(pk))
	}
	if err := km.storage.SaveHighestProposal(pk, minBlockData); err != nil {
		return errors.Wrapf(err, "could not save minimal highest proposal for %s", string(pk))
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
		pkDecoded, err := hex.DecodeString(pubKey)
		if err != nil {
			return errors.Wrap(err, "could not hex decode share public key")
		}
		if err := km.storage.RemoveHighestAttestation(pkDecoded); err != nil {
			return errors.Wrap(err, "could not remove highest attestation")
		}
		if err := km.storage.RemoveHighestProposal(pkDecoded); err != nil {
			return errors.Wrap(err, "could not remove highest proposal")
		}
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

func minimalAttProtectionData(source, target prysmtypes.Epoch) *eth.AttestationData {
	return &eth.AttestationData{
		BeaconBlockRoot: make([]byte, 32),
		Source: &eth.Checkpoint{
			Epoch: source,
			Root:  make([]byte, 32),
		},
		Target: &eth.Checkpoint{
			Epoch: target,
			Root:  make([]byte, 32),
		},
	}
}

func minimalBlockProtectionData(slot prysmtypes.Slot) *eth.BeaconBlock {
	return &eth.BeaconBlock{
		Slot:       slot,
		ParentRoot: make([]byte, 32),
		StateRoot:  make([]byte, 32),
		Body: &eth.BeaconBlockBody{
			RandaoReveal: make([]byte, 96),
			Eth1Data: &eth.Eth1Data{
				DepositRoot: make([]byte, 32),
				BlockHash:   make([]byte, 32),
			},
			Graffiti:          make([]byte, 32),
			ProposerSlashings: []*eth.ProposerSlashing{},
			AttesterSlashings: []*eth.AttesterSlashing{},
			Attestations:      []*eth.Attestation{},
			Deposits:          []*eth.Deposit{},
			VoluntaryExits:    []*eth.SignedVoluntaryExit{},
		},
	}
}

// specAttDataToPrysmAttData a simple func converting between data types
func specAttDataToPrysmAttData(data *spec.AttestationData) *eth.AttestationData {
	// TODO - adopt github.com/attestantio/go-eth2-client in eth2-key-manager
	return &eth.AttestationData{
		Slot:            prysmtypes.Slot(data.Slot),
		CommitteeIndex:  prysmtypes.CommitteeIndex(data.Index),
		BeaconBlockRoot: data.BeaconBlockRoot[:],
		Source: &eth.Checkpoint{
			Epoch: prysmtypes.Epoch(data.Source.Epoch),
			Root:  data.Source.Root[:],
		},
		Target: &eth.Checkpoint{
			Epoch: prysmtypes.Epoch(data.Target.Epoch),
			Root:  data.Target.Root[:],
		},
	}
}

func specBeaconBlockToPrysmBlock(b *bellatrix.BeaconBlock) *eth.BeaconBlock {
	return &eth.BeaconBlock{
		ProposerIndex: prysmtypes.ValidatorIndex(b.ProposerIndex),
		Slot:          prysmtypes.Slot(b.Slot),
		ParentRoot:    b.ParentRoot[:],
		StateRoot:     b.StateRoot[:],
		Body: &eth.BeaconBlockBody{
			RandaoReveal: b.Body.RANDAOReveal[:],
			Eth1Data: &eth.Eth1Data{
				DepositRoot:  b.Body.ETH1Data.DepositRoot[:],
				DepositCount: b.Body.ETH1Data.DepositCount,
				BlockHash:    b.Body.ETH1Data.BlockHash,
			},
			Graffiti: b.Body.Graffiti,
			// fields not used to check the slashing
			ProposerSlashings: []*eth.ProposerSlashing{},
			AttesterSlashings: []*eth.AttesterSlashing{},
			Attestations:      []*eth.Attestation{},
			Deposits:          []*eth.Deposit{},
			VoluntaryExits:    []*eth.SignedVoluntaryExit{},
		},
	}
}
