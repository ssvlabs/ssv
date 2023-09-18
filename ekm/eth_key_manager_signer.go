package ekm

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	eth2keymanager "github.com/bloxapp/eth2-key-manager"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/signer"
	slashingprotection "github.com/bloxapp/eth2-key-manager/slashing_protection"
	"github.com/bloxapp/eth2-key-manager/wallets"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	// minSPAttestationEpochGap is the minimum epoch distance used for slashing protection in attestations.
	// It defines the smallest allowable gap between the source and target epochs in an existing attestation
	// and those in a new attestation, helping to prevent slashable offenses.
	minSPAttestationEpochGap = phase0.Epoch(0)
	// minSPProposalSlotGap is the minimum slot distance used for slashing protection in block proposals.
	// It defines the smallest allowable gap between the current slot and the slot of a new block proposal,
	// helping to prevent slashable offenses.
	minSPProposalSlotGap = phase0.Slot(0)
)

type ethKeyManagerSigner struct {
	wallet            core.Wallet
	walletLock        *sync.RWMutex
	signer            signer.ValidatorSigner
	storage           Storage
	domain            spectypes.DomainType
	slashingProtector core.SlashingProtector
	builderProposals  bool
}

// StorageProvider provides the underlying KeyManager storage.
type StorageProvider interface {
	ListAccounts() ([]core.ValidatorAccount, error)
	RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error)
	RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error)
	BumpSlashingProtection(pubKey []byte) error
}

// NewETHKeyManagerSigner returns a new instance of ethKeyManagerSigner
func NewETHKeyManagerSigner(logger *zap.Logger, db basedb.Database, network networkconfig.NetworkConfig, builderProposals bool, encryptionKey string) (spectypes.KeyManager, error) {
	signerStore := NewSignerStorage(db, network.Beacon, logger)
	if encryptionKey != "" {
		err := signerStore.SetEncryptionKey(encryptionKey)
		if err != nil {
			return nil, err
		}
	}
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

	slashingProtector := slashingprotection.NewNormalProtection(signerStore)
	beaconSigner := signer.NewSimpleSigner(wallet, slashingProtector, core.Network(network.Beacon.GetBeaconNetwork()))

	return &ethKeyManagerSigner{
		wallet:            wallet,
		walletLock:        &sync.RWMutex{},
		signer:            beaconSigner,
		storage:           signerStore,
		domain:            network.Domain,
		slashingProtector: slashingProtector,
		builderProposals:  builderProposals,
	}, nil
}

func (km *ethKeyManagerSigner) ListAccounts() ([]core.ValidatorAccount, error) {
	return km.storage.ListAccounts()
}

func (km *ethKeyManagerSigner) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	return km.storage.RetrieveHighestAttestation(pubKey)
}

func (km *ethKeyManagerSigner) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	return km.storage.RetrieveHighestProposal(pubKey)
}

func (km *ethKeyManagerSigner) SignBeaconObject(obj ssz.HashRoot, domain phase0.Domain, pk []byte, domainType phase0.DomainType) (spectypes.Signature, [32]byte, error) {
	sig, rootSlice, err := km.signBeaconObject(obj, domain, pk, domainType)
	if err != nil {
		return nil, [32]byte{}, err
	}
	var root [32]byte
	copy(root[:], rootSlice)
	return sig, root, nil
}

func (km *ethKeyManagerSigner) signBeaconObject(obj ssz.HashRoot, domain phase0.Domain, pk []byte, domainType phase0.DomainType) (spectypes.Signature, []byte, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	switch domainType {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, nil, errors.New("could not cast obj to AttestationData")
		}
		return km.signer.SignBeaconAttestation(data, domain, pk)
	case spectypes.DomainProposer:
		if km.builderProposals {
			var vBlindedBlock *api.VersionedBlindedBeaconBlock
			switch v := obj.(type) {
			case *apiv1bellatrix.BlindedBeaconBlock:
				vBlindedBlock = &api.VersionedBlindedBeaconBlock{
					Version:   spec.DataVersionBellatrix,
					Bellatrix: v,
				}
				return km.signer.SignBlindedBeaconBlock(vBlindedBlock, domain, pk)
			case *apiv1capella.BlindedBeaconBlock:
				vBlindedBlock = &api.VersionedBlindedBeaconBlock{
					Version: spec.DataVersionCapella,
					Capella: v,
				}
				return km.signer.SignBlindedBeaconBlock(vBlindedBlock, domain, pk)
			}
		}

		var vBlock *spec.VersionedBeaconBlock
		switch v := obj.(type) {
		case *phase0.BeaconBlock:
			vBlock = &spec.VersionedBeaconBlock{
				Version: spec.DataVersionPhase0,
				Phase0:  v,
			}
		case *altair.BeaconBlock:
			vBlock = &spec.VersionedBeaconBlock{
				Version: spec.DataVersionAltair,
				Altair:  v,
			}
		case *bellatrix.BeaconBlock:
			vBlock = &spec.VersionedBeaconBlock{
				Version:   spec.DataVersionBellatrix,
				Bellatrix: v,
			}
		case *capella.BeaconBlock:
			vBlock = &spec.VersionedBeaconBlock{
				Version: spec.DataVersionCapella,
				Capella: v,
			}
		default:
			return nil, nil, fmt.Errorf("obj type is unknown: %T", obj)
		}

		return km.signer.SignBeaconBlock(vBlock, domain, pk)
	case spectypes.DomainAggregateAndProof:
		data, ok := obj.(*phase0.AggregateAndProof)
		if !ok {
			return nil, nil, errors.New("could not cast obj to AggregateAndProof")
		}
		return km.signer.SignAggregateAndProof(data, domain, pk)
	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZUint64")
		}

		return km.signer.SignSlot(phase0.Slot(data), domain, pk)
	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZUint64")
		}

		return km.signer.SignEpoch(phase0.Epoch(data), domain, pk)
	case spectypes.DomainSyncCommittee:
		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZBytes")
		}
		return km.signer.SignSyncCommittee(data, domain, pk)
	case spectypes.DomainSyncCommitteeSelectionProof:
		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}
		return km.signer.SignSyncCommitteeSelectionData(data, domain, pk)
	case spectypes.DomainContributionAndProof:
		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return nil, nil, errors.New("could not cast obj to ContributionAndProof")
		}
		return km.signer.SignSyncCommitteeContributionAndProof(data, domain, pk)
	case spectypes.DomainApplicationBuilder:
		var data *api.VersionedValidatorRegistration
		switch v := obj.(type) {
		case *eth2apiv1.ValidatorRegistration:
			data = &api.VersionedValidatorRegistration{
				Version: spec.BuilderVersionV1,
				V1:      v,
			}
		default:
			return nil, nil, fmt.Errorf("obj type is unknown: %T", obj)
		}
		return km.signer.SignRegistration(data, domain, pk)
	default:
		return nil, nil, errors.New("domain unknown")
	}
}

func (km *ethKeyManagerSigner) IsAttestationSlashable(pk []byte, data *phase0.AttestationData) error {
	if val, err := km.slashingProtector.IsSlashableAttestation(pk, data); err != nil || val != nil {
		if err != nil {
			return err
		}
		return errors.Errorf("slashable attestation (%s), not signing", val.Status)
	}
	return nil
}

func (km *ethKeyManagerSigner) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	status, err := km.slashingProtector.IsSlashableProposal(pk, slot)
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

	sig, err := account.ValidationKeySign(root[:])
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
		if err := km.BumpSlashingProtection(shareKey.GetPublicKey().Serialize()); err != nil {
			return errors.Wrap(err, "could not bump slashing protection")
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

// BumpSlashingProtection updates the slashing protection data for a given public key.
func (km *ethKeyManagerSigner) BumpSlashingProtection(pubKey []byte) error {
	currentSlot := km.storage.BeaconNetwork().EstimatedCurrentSlot()

	// Update highest attestation data for slashing protection.
	if err := km.updateHighestAttestation(pubKey, currentSlot); err != nil {
		return err
	}

	// Update highest proposal data for slashing protection.
	if err := km.updateHighestProposal(pubKey, currentSlot); err != nil {
		return err
	}

	return nil
}

// updateHighestAttestation updates the highest attestation data for slashing protection.
func (km *ethKeyManagerSigner) updateHighestAttestation(pubKey []byte, slot phase0.Slot) error {
	// Retrieve the highest attestation data stored for the given public key.
	retrievedHighAtt, found, err := km.RetrieveHighestAttestation(pubKey)
	if err != nil {
		return fmt.Errorf("could not retrieve highest attestation: %w", err)
	}

	currentEpoch := km.storage.BeaconNetwork().EstimatedEpochAtSlot(slot)
	minimalSP := km.computeMinimalAttestationSP(currentEpoch)

	// Check if the retrieved highest attestation data is valid and not outdated.
	if found && retrievedHighAtt != nil {
		if retrievedHighAtt.Source.Epoch >= minimalSP.Source.Epoch || retrievedHighAtt.Target.Epoch >= minimalSP.Target.Epoch {
			return nil
		}
	}

	// At this point, either the retrieved attestation data was not found, or it was outdated.
	// In either case, we update it to the minimal slashing protection data.
	if err := km.storage.SaveHighestAttestation(pubKey, minimalSP); err != nil {
		return fmt.Errorf("could not save highest attestation: %w", err)
	}

	return nil
}

// updateHighestProposal updates the highest proposal slot for slashing protection.
func (km *ethKeyManagerSigner) updateHighestProposal(pubKey []byte, slot phase0.Slot) error {
	// Retrieve the highest proposal slot stored for the given public key.
	retrievedHighProp, found, err := km.RetrieveHighestProposal(pubKey)
	if err != nil {
		return fmt.Errorf("could not retrieve highest proposal: %w", err)
	}

	minimalSPSlot := km.computeMinimalProposerSP(slot)

	// Check if the retrieved highest proposal slot is valid and not outdated.
	if found && retrievedHighProp != 0 {
		if retrievedHighProp >= minimalSPSlot {
			return nil
		}
	}

	// At this point, either the retrieved proposal slot was not found, or it was outdated.
	// In either case, we update it to the minimal slashing protection slot.
	if err := km.storage.SaveHighestProposal(pubKey, minimalSPSlot); err != nil {
		return fmt.Errorf("could not save highest proposal: %w", err)
	}

	return nil
}

// computeMinimalAttestationSP calculates the minimal safe attestation data for slashing protection.
// It takes the current epoch as an argument and returns an AttestationData object with the minimal safe source and target epochs.
func (km *ethKeyManagerSigner) computeMinimalAttestationSP(epoch phase0.Epoch) *phase0.AttestationData {
	// Calculate the highest safe target epoch based on the current epoch and a predefined minimum distance.
	highestTarget := epoch + minSPAttestationEpochGap
	// The highest safe source epoch is one less than the highest target epoch.
	highestSource := highestTarget - 1

	// Return a new AttestationData object with the calculated source and target epochs.
	return &phase0.AttestationData{
		Source: &phase0.Checkpoint{
			Epoch: highestSource,
		},
		Target: &phase0.Checkpoint{
			Epoch: highestTarget,
		},
	}
}

// computeMinimalProposerSP calculates the minimal safe slot for a block proposal to avoid slashing.
// It takes the current slot as an argument and returns the minimal safe slot.
func (km *ethKeyManagerSigner) computeMinimalProposerSP(slot phase0.Slot) phase0.Slot {
	// Calculate the highest safe proposal slot based on the current slot and a predefined minimum distance.
	return slot + minSPProposalSlotGap
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
