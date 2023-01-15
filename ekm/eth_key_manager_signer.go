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

type ethKeyManagerSigner struct {
	wallet            core.Wallet
	walletLock        *sync.RWMutex
	storage           *signerStorage
	signingUtils      beacon.Beacon
	domain            spectypes.DomainType
	slashingProtector core.SlashingProtector
}

// NewETHKeyManagerSigner returns a new instance of ethKeyManagerSigner
func NewETHKeyManagerSigner(db basedb.IDb, signingUtils beaconprotocol.Beacon, network beaconprotocol.Network, domain spectypes.DomainType) (spectypes.KeyManager, error) {
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

func (km *ethKeyManagerSigner) SignBeaconObject(obj ssz.HashRoot, domain spec.Domain, pk []byte, role spectypes.BeaconRole) (spectypes.Signature, []byte, error) {
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

	if err := km.slashingProtection(pk, obj, role); err != nil {
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

func (km *ethKeyManagerSigner) slashingProtection(pk []byte, obj ssz.HashRoot, role spectypes.BeaconRole) error {
	switch role {
	case spectypes.BNRoleAttester:
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

	case spectypes.BNRoleProposer:
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
		if err := km.storage.SaveHighestAttestation(shareKey.GetPublicKey().Serialize(), zeroSlotAttestation); err != nil {
			return errors.Wrap(err, "could not save zero highest attestation")
		}
		if err := km.storage.SaveHighestProposal(shareKey.GetPublicKey().Serialize(), zeroSlotBeaconBlock); err != nil {
			return errors.Wrap(err, "could not save zero highest proposal")
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

// zeroSlotAttestation is a place holder beacon block representing all zero values
var zeroSlotBeaconBlock = &eth.BeaconBlock{
	Slot:          0,
	ProposerIndex: 1,
	ParentRoot: []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	},
	StateRoot: []byte{
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	},
	Body: &eth.BeaconBlockBody{
		RandaoReveal: []byte{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		},
		Eth1Data: &eth.Eth1Data{
			DepositRoot: []byte{
				0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
				0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
			},
			DepositCount: 16384,
			BlockHash: []byte{
				0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
				0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
			},
		},
		Graffiti: []byte{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		},
		ProposerSlashings: []*eth.ProposerSlashing{},
		AttesterSlashings: []*eth.AttesterSlashing{},
		Attestations:      []*eth.Attestation{},
		Deposits:          []*eth.Deposit{},
		VoluntaryExits:    []*eth.SignedVoluntaryExit{},
	},
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
