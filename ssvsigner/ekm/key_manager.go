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

	// RemoveShare unregisters a validator share from the key manager and deletes its associated
	// slashing protection records (attestation and proposal) from the store.
	// Implementations are expected to perform this cleanup to prevent stale protection data
	// from persisting after the validator is no longer active, and to support safe re-adding later.
	RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error
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
