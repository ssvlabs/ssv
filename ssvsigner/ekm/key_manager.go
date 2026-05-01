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
	BumpSlashingProtection(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error

	// AddShare registers a validator share (public and encrypted private key) with the key manager.
	// Implementations should always call BumpSlashingProtection during this process.
	// It ensures slashing protection records (attestation and proposal) are present and up to date,
	// updating them only if they are missing or fall below a minimal safe threshold.
	// This prevents the validator from signing messages that could be considered slashable
	// due to absent or outdated protection data.
	AddShare(ctx context.Context, txn ReadWriteTxn, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error

	// RemoveShare unregisters a validator share from the key manager and deletes its associated
	// slashing protection records (attestation and proposal) from the store.
	// Implementations are expected to perform this cleanup to prevent stale protection data
	// from persisting after the validator is no longer active, and to support safe re-adding later.
	RemoveShare(ctx context.Context, txn ReadWriteTxn, pubKey phase0.BLSPubKey) error
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
	// UpdateHighestProposal records `slot` as the highest slot the operator
	// has signed a block for under `pubKey`. Future IsBeaconBlockSlashable
	// checks compare against this record.
	//
	// Callers that go through SignBeaconObject get this update for free
	// inside that path. Callers that sign outside of SignBeaconObject —
	// notably the TBFT proposer, where the threshold-reconstructed master
	// signature isn't produced via the EKM signing path — must invoke
	// this explicitly after a successful submission.
	UpdateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error
}

// ShareBytesProvider is implemented by signers that can hand back the
// raw BLS share bytes for a given pubkey. Used by callers that need the
// share material for non-Eth2 BLS operations under the DST-trick
// approach (e.g. TBFT's IBE-tag KyberSigner — see the IBE-INTEGRATION
// doc).
//
// Only LocalKeyManager implements this. RemoteKeyManager intentionally
// does not — share material never leaves the remote signer in that
// configuration. Callers must type-assert and gracefully handle the
// not-implemented case (e.g. by skipping TBFT setup for the relevant
// runner). A future ssv-signer protocol extension may expose
// drand-DST signing remotely so the share bytes never need to be
// exposed locally; that's the production replacement for this
// interface.
type ShareBytesProvider interface {
	GetShareBytes(pubKey phase0.BLSPubKey) ([]byte, error)
}
