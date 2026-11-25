package ekm

import (
	"bytes"
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// KeyManager is the main interface for managing validator shares and performing slashing protection.
// It embeds BeaconSigner, for signing beacon objects and checking whether an attestation or block
// is slashable.
type KeyManager interface {
	BeaconSigner

	// BumpSlashingProtection raises the share's attestation and proposal records to the minimal safe
	// values for the current slot/epoch, if they are missing or lower. It writes them immediately,
	// under the lock that excludes signing the share, never through a caller's transaction, whose
	// commit lands after the lock is released. A bump only raises records, so rolling back the
	// caller's transaction afterwards is harmless.
	//
	// The minimal values refuse an attestation whose target isn't after the current epoch or whose
	// source is before the previous one, and a block that isn't after the current slot. A validator
	// that already signs with the share key then can't attest on this operator for the current
	// epoch, unless it already has, nor for later
	// ones until the previous epoch, or a later one, is justified. Callers must not bump a key that
	// another validator signs with: the event handler skips the bump on cluster reactivation for
	// such a key.
	BumpSlashingProtection(pubKey phase0.BLSPubKey) error

	// AddShare registers a validator share (public and encrypted private key) with the key manager.
	// Implementations must also bump the share's slashing records, with BumpSlashingProtection or
	// directly under the same lock, so the validator never signs from missing or outdated records.
	// They bump even when they already hold the share, as its records may still be missing, e.g.
	// when a remote signer kept the key across a crash or a wipe of the node's database. Callers
	// must therefore not add a share key that another validator signs with (see
	// BumpSlashingProtection): the event handler rejects such a registration before AddShare.
	//
	// If the share cannot be decrypted or validated, implementations MUST return an error that
	// unwraps to a by-value ssvsigner.ShareDecryptionError. Callers use it to classify the failure
	// as a skippable malformed event; otherwise the failure is fatal and crash-loops registry sync.
	AddShare(ctx context.Context, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error

	// RemoveShare unregisters a validator share from the key manager. It keeps the share's slashing
	// records, so a later re-add of the same share (the same keyshares re-registered, e.g. after a
	// DKG resign) can never start below what this node already signed.
	//
	// The flip side: a re-add doesn't clear a record that is too high (e.g. raised under a wrong
	// clock), which refuses signatures until the real clock passes it. To clear it early, delete the
	// record by hand (signing then fails until the next bump, e.g. a re-add) or wipe the node's
	// database and resync, but only if the node signed nothing above the real clock: the next bump
	// puts the record back at the real clock, below any such signature, and a conflicting one for the
	// same slot or target epoch would then pass. The records' keys are
	// <network>signer_data-highest_att-<share> and <network>signer_data-highest_prop-<share>, where
	// <network> is the beacon network's name and <share> is the share's raw 48-byte public key (see
	// highestAttPrefix and highestProposalPrefix).
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
		obj spectypes.HashRoot,
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

// checkDomainType refuses a domain whose DomainType (its first four bytes: compute_domain =
// domain_type ‖ fork_data_root[:28]) differs from signatureDomain. Slashing protection is picked by
// signatureDomain but the signature is made under domain, so the two must agree.
// Callers derive domain from signatureDomain, so a mismatch is always a caller bug.
func checkDomainType(domain phase0.Domain, signatureDomain phase0.DomainType) error {
	if !bytes.Equal(domain[:4], signatureDomain[:]) {
		return fmt.Errorf("domain type %x does not match signature domain %x", domain[:4], signatureDomain[:])
	}
	return nil
}
