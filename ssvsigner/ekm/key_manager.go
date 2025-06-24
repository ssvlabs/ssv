package ekm

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type ShareDecryptionError struct {
	Err error
}

func (e ShareDecryptionError) Error() string {
	if e.Err == nil {
		return "share decryption error: nil"
	}
	return "share decryption error: " + e.Err.Error()
}

func (e ShareDecryptionError) Unwrap() error {
	return e.Err
}

// KeyManager is the main interface for managing validator shares and performing slashing protection.
// It embeds BeaconSigner (for signing beacon messages and checking whether attestation or beacon block are slashable)
// and slashingProtector (for slashing checks and updates).
//
// TODO(SSV-15): This interface includes temporary methods (ArchiveSlashingProtection, ApplyArchivedSlashingProtection)
// to work around an architectural issue where validator share regeneration produces new public keys,
// making existing slashing protection data (keyed by share pubkey) inaccessible upon re-registration.
//
// The current workaround archives slashing protection by validator pubkey during removal and
// restores it during re-addition. This addresses an edge case where a majority fork (like Holesky)
// could lead to slashing if validators are removed and re-added.
//
// Long-term solutions under consideration:
// 1. Deterministic share generation based on validator key
// 2. Validator-centric slashing protection storage (align with EIP-3076)
// 3. Consistent key derivation scheme for share keys
//
// These temporary methods should be removed once a proper architectural solution is implemented.
type KeyManager interface {
	BeaconSigner
	slashingProtector

	// AddShare registers a validator share (public and encrypted private key) with the key manager.
	// Implementations should always call BumpSlashingProtection during this process.
	// It ensures slashing protection records (attestation and proposal) are present and up to date,
	// updating them only if they are missing or fall below a minimal safe threshold.
	// This prevents the validator from signing messages that could be considered slashable
	// due to absent or outdated protection data.
	AddShare(ctx context.Context, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error

	// RemoveShare unregisters a validator share from the key manager.
	// Implementations should consider preserving slashing protection records
	// (attestation and proposal) to prevent slashing if the share is re-added later.
	// This is especially important for local key managers where the local database
	// is the only safeguard against double signing.
	RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error

	// ArchiveSlashingProtection preserves slashing protection data keyed by validator public key.
	// This method is part of the temporary solution for audit finding SSV-15.
	// It should be called before removing a validator share to maintain slashing protection
	// history when the validator is subsequently re-added with regenerated shares.
	//
	// TODO(SSV-15): Remove this method once proper architectural solution is implemented.
	ArchiveSlashingProtection(validatorPubKey []byte, sharePubKey []byte) error

	// ApplyArchivedSlashingProtection applies archived slashing protection data for a validator.
	// This method is part of the temporary solution for audit finding SSV-15.
	// It should be called when adding a share to ensure slashing protection continuity
	// across validator re-registration cycles. It uses maximum value logic to prevent regression.
	//
	// TODO(SSV-15): Remove this method once proper architectural solution is implemented.
	ApplyArchivedSlashingProtection(validatorPubKey []byte, sharePubKey phase0.BLSPubKey) error
}

// BeaconSigner provides methods for signing beacon-chain objects.
// Attestations and blocks are checked for slashing conditions
// through the slashingProtector interface before signing.
//
// SignBeaconObject distinguishes object types by the passed domainType.
type BeaconSigner interface {
	// SignBeaconObject returns the signature for the given object along with
	// the computed root. If slashable, it should return an error.
	SignBeaconObject(
		ctx context.Context,
		obj ssz.HashRoot,
		domain phase0.Domain,
		pubKey phase0.BLSPubKey,
		slot phase0.Slot,
		signatureDomain phase0.DomainType,
	) (spectypes.Signature, phase0.Root, error)
	// IsAttestationSlashable returns error if attestation is slashable
	IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error
	// IsBeaconBlockSlashable returns error if the given block is slashable
	IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error
}
