package ekm

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	eth2keymanager "github.com/ssvlabs/eth2-key-manager"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/signer"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"github.com/ssvlabs/eth2-key-manager/wallets"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// LocalKeyManager implements KeyManager by storing and operating on BLS keys locally.
// It relies on eth2-key-manager for signing and performing slashing checks before signing.
//
// It uses the operator's private key to decrypt incoming shares.
//
// The underlying wallet data is stored in the provided db.
// Signing along with slashing protection are managed with eth2-key-manager's slashingprotection.NewNormalProtection.
//
// There are two wrappers for slashingprotection.NewNormalProtection:
//   - signer.SimpleSigner for internal checks and updates
//     (we cannot use SlashingProtector because slashing protection is embedded to signing methods)
//   - SlashingProtector for external checks and updates
//
// All slashing checks are performed prior to any signing attempt. If a slashable
// condition is detected, the signing method will return an error.
type LocalKeyManager struct {
	wallet            core.Wallet
	walletLock        *sync.RWMutex
	signer            signer.ValidatorSigner
	domain            spectypes.DomainType
	operatorDecrypter keys.OperatorDecrypter
	slashingProtector
}

// NewLocalKeyManager returns a new LocalKeyManager.
func NewLocalKeyManager(
	logger *zap.Logger,
	db basedb.Database,
	network networkconfig.NetworkConfig,
	operatorPrivKey keys.OperatorPrivateKey,
) (*LocalKeyManager, error) {
	signerStore := NewSignerStorage(db, network.Beacon, logger)
	if err := signerStore.SetEncryptionKey(operatorPrivKey.EKMHash()); err != nil {
		return nil, err
	}

	protection := slashingprotection.NewNormalProtection(signerStore)

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

	beaconSigner := signer.NewSimpleSigner(wallet, protection, core.Network(network.Beacon.GetBeaconNetwork()))

	return &LocalKeyManager{
		wallet:            wallet,
		walletLock:        &sync.RWMutex{},
		signer:            beaconSigner,
		domain:            network.DomainType,
		slashingProtector: NewSlashingProtector(logger, signerStore, protection),
		operatorDecrypter: operatorPrivKey,
	}, nil
}

// SignBeaconObject implements BeaconSigner. It locks the wallet, checks
// domain type, converts the input object to the correct versioned data,
// runs, then delegates slashing protection checks for attestations and blocks
// as well as signing to eth2-key-manager's signer.
//
// It returns the signature and the computed root on success.
func (km *LocalKeyManager) SignBeaconObject(
	_ context.Context,
	obj ssz.HashRoot,
	domain phase0.Domain,
	pubKey phase0.BLSPubKey,
	_ phase0.Slot,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, phase0.Root, error) {
	sig, rootSlice, err := km.signBeaconObject(obj, domain, pubKey, signatureDomain)
	if err != nil {
		return nil, phase0.Root{}, err
	}
	var root phase0.Root
	copy(root[:], rootSlice)
	return sig, root, nil
}

func (km *LocalKeyManager) signBeaconObject(
	obj ssz.HashRoot,
	domain phase0.Domain,
	pubKey phase0.BLSPubKey,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, []byte, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	switch signatureDomain {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, nil, errors.New("could not cast obj to AttestationData")
		}
		return km.signer.SignBeaconAttestation(data, domain, pubKey[:])
	case spectypes.DomainProposer:
		switch v := obj.(type) {
		case *capella.BeaconBlock:
			vBlock := &spec.VersionedBeaconBlock{
				Version: spec.DataVersionCapella,
				Capella: v,
			}
			return km.signer.SignBeaconBlock(vBlock, domain, pubKey[:])
		case *deneb.BeaconBlock:
			vBlock := &spec.VersionedBeaconBlock{
				Version: spec.DataVersionDeneb,
				Deneb:   v,
			}
			return km.signer.SignBeaconBlock(vBlock, domain, pubKey[:])
		case *electra.BeaconBlock:
			vBlock := &spec.VersionedBeaconBlock{
				Version: spec.DataVersionElectra,
				Electra: v,
			}
			return km.signer.SignBeaconBlock(vBlock, domain, pubKey[:])
		case *apiv1capella.BlindedBeaconBlock:
			vBlindedBlock := &api.VersionedBlindedBeaconBlock{
				Version: spec.DataVersionCapella,
				Capella: v,
			}
			return km.signer.SignBlindedBeaconBlock(vBlindedBlock, domain, pubKey[:])
		case *apiv1deneb.BlindedBeaconBlock:
			vBlindedBlock := &api.VersionedBlindedBeaconBlock{
				Version: spec.DataVersionDeneb,
				Deneb:   v,
			}
			return km.signer.SignBlindedBeaconBlock(vBlindedBlock, domain, pubKey[:])
		case *apiv1electra.BlindedBeaconBlock:
			vBlindedBlock := &api.VersionedBlindedBeaconBlock{
				Version: spec.DataVersionElectra,
				Electra: v,
			}
			return km.signer.SignBlindedBeaconBlock(vBlindedBlock, domain, pubKey[:])
		default:
			return nil, nil, fmt.Errorf("obj type is unknown: %T", obj)
		}

	case spectypes.DomainVoluntaryExit:
		data, ok := obj.(*phase0.VoluntaryExit)
		if !ok {
			return nil, nil, errors.New("could not cast obj to VoluntaryExit")
		}
		return km.signer.SignVoluntaryExit(data, domain, pubKey[:])
	case spectypes.DomainAggregateAndProof:
		return km.signer.SignAggregateAndProof(obj, domain, pubKey[:])
	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZUint64")
		}

		return km.signer.SignSlot(phase0.Slot(data), domain, pubKey[:])
	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZUint64")
		}

		return km.signer.SignEpoch(phase0.Epoch(data), domain, pubKey[:])
	case spectypes.DomainSyncCommittee:
		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SSZBytes")
		}
		return km.signer.SignSyncCommittee(data, domain, pubKey[:])
	case spectypes.DomainSyncCommitteeSelectionProof:
		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return nil, nil, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}
		return km.signer.SignSyncCommitteeSelectionData(data, domain, pubKey[:])
	case spectypes.DomainContributionAndProof:
		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return nil, nil, errors.New("could not cast obj to ContributionAndProof")
		}
		return km.signer.SignSyncCommitteeContributionAndProof(data, domain, pubKey[:])
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
		return km.signer.SignRegistration(data, domain, pubKey[:])
	default:
		return nil, nil, errors.New("domain unknown")
	}
}

// AddShare decrypts the provided share private key (encryptedSharePrivKey)
// using the operatorDecrypter, verifies that it matches sharePubKey, and
// saves it to the local wallet. It also calls BumpSlashingProtection to
// ensure slashing records for this share are up to date.
func (km *LocalKeyManager) AddShare(_ context.Context, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error {
	km.walletLock.Lock()
	defer km.walletLock.Unlock()

	sharePrivKeyHex, err := km.operatorDecrypter.Decrypt(encryptedPrivKey)
	if err != nil {
		return ShareDecryptionError{Err: fmt.Errorf("decrypt: %w", err)}
	}

	sharePrivKey := &bls.SecretKey{}
	if err := sharePrivKey.SetHexString(string(sharePrivKeyHex)); err != nil {
		return ShareDecryptionError{Err: fmt.Errorf("decode hex: %w", err)}
	}

	if !bytes.Equal(sharePrivKey.GetPublicKey().Serialize(), pubKey[:]) {
		return ShareDecryptionError{Err: errors.New("share private key does not match public key")}
	}

	acc, err := km.wallet.AccountByPublicKey(sharePrivKey.GetPublicKey().SerializeToHexStr())
	if err != nil && err.Error() != "account not found" {
		return fmt.Errorf("could not check share existence: %w", err)
	}
	if acc == nil {
		if err := km.BumpSlashingProtection(phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())); err != nil {
			return fmt.Errorf("could not bump slashing protection: %w", err)
		}
		if err := km.saveShare(sharePrivKey); err != nil {
			return fmt.Errorf("could not save share: %w", err)
		}
	}

	return nil
}

// RemoveShare removes the share from the local wallet, clears the associated
// slashing-protection records (highest attestation/proposal) for the given
// public key, and returns an error on any storage issue.
func (km *LocalKeyManager) RemoveShare(_ context.Context, pubKey phase0.BLSPubKey) error {
	km.walletLock.Lock()
	defer km.walletLock.Unlock()

	pubKeyHex := hex.EncodeToString(pubKey[:]) // pubKey.String() would add the "0x" prefix so we cannot use it

	acc, err := km.wallet.AccountByPublicKey(pubKeyHex)
	if err != nil && err.Error() != "account not found" {
		return fmt.Errorf("could not check share existence: %w", err)
	}
	if acc != nil {
		if err := km.RemoveHighestAttestation(pubKey); err != nil {
			return fmt.Errorf("could not remove highest attestation: %w", err)
		}
		if err := km.RemoveHighestProposal(pubKey); err != nil {
			return fmt.Errorf("could not remove highest proposal: %w", err)
		}
		if err := km.wallet.DeleteAccountByPublicKey(pubKeyHex); err != nil {
			return fmt.Errorf("could not delete share: %w", err)
		}
	}
	return nil
}

func (km *LocalKeyManager) saveShare(privKey *bls.SecretKey) error {
	key, err := core.NewHDKeyFromPrivateKey(privKey.Serialize(), "")
	if err != nil {
		return fmt.Errorf("could not generate HDKey: %w", err)
	}
	account := wallets.NewValidatorAccount("", key, nil, "", nil)
	if err := km.wallet.AddValidatorAccount(account); err != nil {
		return fmt.Errorf("could not save new account: %w", err)
	}
	return nil
}
