package ekm

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
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
	logger            *zap.Logger
	wallet            core.Wallet
	walletLock        *sync.RWMutex
	signer            signer.ValidatorSigner
	operatorDecrypter keys.OperatorDecrypter
	slashingProtector slashingProtector

	// shareBytesMu guards shareBytes. Separate from walletLock because
	// the read-path (TBFT signer construction) is called on a hotter
	// path than wallet operations and shouldn't block on AddShare /
	// RemoveShare wallet I/O.
	shareBytesMu sync.RWMutex
	// shareBytes maps validator share pubkeys to the raw 32-byte BLS
	// scalar the operator's share corresponds to. Populated at AddShare
	// time, cleared at RemoveShare time. Used by callers that need the
	// share material for non-Eth2 BLS operations under the DST-trick
	// approach (TBFT's KyberSigner, see docs/IBE-INTEGRATION.md).
	//
	// Local-mode-only — RemoteKeyManager keeps shares behind ssv-signer
	// and does not implement ShareBytesProvider.
	shareBytes map[phase0.BLSPubKey][]byte
}

// NewLocalKeyManager returns a new LocalKeyManager.
func NewLocalKeyManager(
	logger *zap.Logger,
	db Database,
	beacon BeaconNetwork,
	operatorPrivKey keys.OperatorPrivateKey,
) (*LocalKeyManager, error) {
	encryptionKey, err := operatorPrivKey.EKMEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("get encryption key: %w", err)
	}

	signerStore := NewSignerStorage(db, beacon.NetworkName(), logger)
	signerStore.SetEncryptionKey(encryptionKey)

	protection := slashingprotection.NewNormalProtection(signerStore)

	options := &eth2keymanager.KeyVaultOptions{}
	options.SetStorage(signerStore)
	options.SetWalletType(core.NDWallet)

	wallet, err := signerStore.OpenWallet()
	if err != nil && !errors.Is(err, errWalletNotFound) {
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

	beaconSigner := signer.NewSimpleSigner(wallet, protection, beacon)

	return &LocalKeyManager{
		logger:            logger,
		wallet:            wallet,
		walletLock:        &sync.RWMutex{},
		signer:            beaconSigner,
		slashingProtector: NewSlashingProtector(logger, beacon, signerStore, protection),
		operatorDecrypter: operatorPrivKey,
		shareBytes:        make(map[phase0.BLSPubKey][]byte),
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
			// Note: Fulu fork reuses electra.BeaconBlock type without structural changes.
			// We always use DataVersionElectra here, while RemoteKeyManager determines
			// version from slot for Web3Signer compatibility. This difference is acceptable
			// because eth2-key-manager uses the domain parameter (not Version field) for
			// signature computation, and the domain already contains fork-specific information.
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
			// Note: Same as regular blocks - Fulu reuses Electra blinded block types
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

func (km *LocalKeyManager) IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error {
	return km.slashingProtector.IsAttestationSlashable(pubKey, attData)
}

func (km *LocalKeyManager) IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	return km.slashingProtector.IsBeaconBlockSlashable(pubKey, slot)
}

func (km *LocalKeyManager) UpdateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	return km.slashingProtector.UpdateHighestProposal(pubKey, slot)
}

func (km *LocalKeyManager) BumpSlashingProtection(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	return km.slashingProtector.BumpSlashingProtectionTxn(txn, pubKey)
}

func (km *LocalKeyManager) RetrieveHighestAttestation(pubKey phase0.BLSPubKey) (*phase0.AttestationData, bool, error) {
	return km.slashingProtector.RetrieveHighestAttestation(pubKey)
}

func (km *LocalKeyManager) RetrieveHighestProposal(pubKey phase0.BLSPubKey) (phase0.Slot, bool, error) {
	return km.slashingProtector.RetrieveHighestProposal(pubKey)
}

func (km *LocalKeyManager) ListAccounts() ([]core.ValidatorAccount, error) {
	return km.slashingProtector.ListAccounts()
}

// AddShare decrypts the provided share private key (encryptedSharePrivKey)
// using the operatorDecrypter, verifies that it matches sharePubKey, and
// saves it to the local wallet. It also bumps slashing protection to
// ensure slashing records for this share are up to date.
func (km *LocalKeyManager) AddShare(
	_ context.Context,
	txn ReadWriteTxn,
	encryptedPrivKey []byte,
	pubKey phase0.BLSPubKey,
) error {
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

	if err := km.slashingProtector.BumpSlashingProtectionTxn(txn, phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	acc, err := km.wallet.AccountByPublicKey(sharePrivKey.GetPublicKey().SerializeToHexStr())
	if err != nil && err.Error() != "account not found" {
		return fmt.Errorf("could not check share existence: %w", err)
	}

	if acc == nil {
		// We don't pass txn because wallet doesn't support transactions (eth2-key-manager doesn't depend on DB entities).
		// However, a rolled back transaction should not be an issue,
		// because the node will crash in this case and then attempt to process the event again but acc won't be nil.
		if err := km.saveAccount(sharePrivKey); err != nil {
			return fmt.Errorf("save account: %w", err)
		}
	}

	// Cache raw share bytes for callers that need them outside the
	// standard signing path (e.g. TBFT's KyberSigner under the DST-trick
	// approach). See ShareBytesProvider for usage and security context.
	km.shareBytesMu.Lock()
	km.shareBytes[pubKey] = append([]byte(nil), sharePrivKey.Serialize()...)
	km.shareBytesMu.Unlock()

	return nil
}

// RemoveShare removes the share from the local wallet, clears the associated
// slashing-protection records (highest attestation/proposal) for the given
// public key, and returns an error on any storage issue.
func (km *LocalKeyManager) RemoveShare(_ context.Context, txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	km.walletLock.Lock()
	defer km.walletLock.Unlock()

	if err := km.slashingProtector.RemoveHighestAttestationTxn(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest attestation: %w", err)
	}
	if err := km.slashingProtector.RemoveHighestProposalTxn(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest proposal: %w", err)
	}

	pubKeyHex := hex.EncodeToString(pubKey[:]) // pubKey.String() would add the "0x" prefix so we cannot use it
	acc, err := km.wallet.AccountByPublicKey(pubKeyHex)
	if err != nil && err.Error() != "account not found" {
		return fmt.Errorf("could not check share existence: %w", err)
	}
	if acc != nil {
		// We don't pass txn because wallet doesn't support transactions (eth2-key-manager doesn't depend on DB entities).
		// However, a rolled back transaction should not be an issue,
		// because the node will crash in this case and then attempt to process the event again but acc will be nil.
		if err := km.wallet.DeleteAccountByPublicKey(pubKeyHex); err != nil {
			return fmt.Errorf("could not delete share: %w", err)
		}
	}

	km.shareBytesMu.Lock()
	delete(km.shareBytes, pubKey)
	km.shareBytesMu.Unlock()
	return nil
}

// GetShareBytes returns the raw 32-byte BLS scalar for the operator's
// share corresponding to `pubKey`. Returns an error if no share for the
// given pubkey is registered.
//
// On a cache miss, falls back to extracting the bytes from the loaded
// wallet account — the share is sitting in memory inside the eth2-key-
// manager `HDAccount.validationKey`, just behind an unexported field.
// We round-trip through its JSON marshaller (the same one the wallet
// uses to persist itself) to read the private-key hex out. This makes
// shares loaded from disk on process restart usable here without
// re-replaying contract events.
//
// Security note: the returned bytes are sensitive material. Callers
// should hold them only as long as needed and avoid logging.
//
// Local-mode only: implementations of `ekm.BeaconSigner` that route to
// remote signers (ssv-signer + Web3Signer) intentionally do NOT
// implement ShareBytesProvider — share bytes never leave those services.
// Callers should type-assert and gracefully handle the not-implemented
// case (e.g. by skipping TBFT-related setup for that runner).
func (km *LocalKeyManager) GetShareBytes(pubKey phase0.BLSPubKey) ([]byte, error) {
	km.shareBytesMu.RLock()
	cached, ok := km.shareBytes[pubKey]
	km.shareBytesMu.RUnlock()
	if ok {
		return append([]byte(nil), cached...), nil
	}

	bytes, err := km.extractShareBytesFromWallet(pubKey)
	if err != nil {
		return nil, err
	}

	km.shareBytesMu.Lock()
	km.shareBytes[pubKey] = bytes
	km.shareBytesMu.Unlock()
	return append([]byte(nil), bytes...), nil
}

// extractShareBytesFromWallet pulls the BLS private-key bytes out of
// the eth2-key-manager wallet account whose validator pubkey matches
// `pubKey`. The wallet stores these bytes in memory (loaded from
// encrypted DB at startup) but only exposes them via JSON marshal/
// unmarshal. We piggyback on that.
//
// Returns ("ekm: no share registered for pubkey ..." error) if no
// wallet account matches.
func (km *LocalKeyManager) extractShareBytesFromWallet(pubKey phase0.BLSPubKey) ([]byte, error) {
	km.walletLock.RLock()
	defer km.walletLock.RUnlock()

	pubKeyHex := hex.EncodeToString(pubKey[:])
	acc, err := km.wallet.AccountByPublicKey(pubKeyHex)
	if err != nil {
		// "account not found" surfaces through this error path; map
		// to the same not-registered shape as the cache miss.
		return nil, fmt.Errorf("ekm: no share registered for pubkey %x", pubKey[:])
	}
	if acc == nil {
		return nil, fmt.Errorf("ekm: no share registered for pubkey %x", pubKey[:])
	}

	// HDAccount.MarshalJSON writes the validation key (an HDKey) as a
	// nested object whose `privKey` is hex-serialized secret-key bytes.
	// This format is stable — it's how the wallet itself round-trips
	// to and from the encrypted DB on every load.
	raw, err := json.Marshal(acc)
	if err != nil {
		return nil, fmt.Errorf("ekm: marshal wallet account: %w", err)
	}
	var aux struct {
		ValidationKey struct {
			PrivKey string `json:"privKey"`
		} `json:"validationKey"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("ekm: parse wallet account JSON: %w", err)
	}
	if aux.ValidationKey.PrivKey == "" {
		return nil, fmt.Errorf("ekm: wallet account missing privKey for pubkey %x", pubKey[:])
	}
	out, err := hex.DecodeString(aux.ValidationKey.PrivKey)
	if err != nil {
		return nil, fmt.Errorf("ekm: decode privKey hex: %w", err)
	}
	return out, nil
}

func (km *LocalKeyManager) saveAccount(privKey *bls.SecretKey) error {
	key, err := core.NewHDKeyFromPrivateKey(privKey.Serialize(), "")
	if err != nil {
		// Not exposing the internal error to prevent leaking private key details.
		return fmt.Errorf("could not generate HDKey")
	}
	account := wallets.NewValidatorAccount("", key, nil, "", nil)
	if err := km.wallet.AddValidatorAccount(account); err != nil {
		return fmt.Errorf("add validator account to wallet: %w", err)
	}
	return nil
}
