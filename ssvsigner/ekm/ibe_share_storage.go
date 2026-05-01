package ekm

import "errors"

// ibe_share_storage.go declares the interfaces and storage schema for
// per-cluster IBE share material used by the TBFT proposer-duty path under
// Option B. See docs/TBFT-DKG-TASKS.md (Phase D) for the full plan; this
// file lands in Phase A3 as the design stub. The concrete implementation
// on LocalKeyManager lands in Phase D.
//
// Why this lives in ssvsigner/ekm:
//
//   - Mirrors ShareBytesProvider, the existing local-mode-only accessor
//     for raw validator-share bytes (see key_manager.go). The IBE share is
//     a similarly-shaped piece of secret material that the TBFT-IBE
//     tag-signing path consumes.
//   - Keeps kyber and DKG types out of ssvsigner. The interface trades in
//     bytes (the kyber-format scalar / point bytes); marshaling /
//     unmarshaling lives in protocol/v2/dkg/ in the main module.
//
// Schema sketch (concrete encoding TBD in Phase D):
//
//   - One BadgerDB record per (clusterID, generation), under a new
//     prefix `signer_data-ibe_share-`. The persisted blob is the
//     concatenation of:
//       - shareBytes        — kyber-format scalar (32 bytes for BLS-12-381)
//       - clusterIBEPubKey  — kyber-format G1 point (~48 bytes)
//       - polyCommits       — length-prefixed list of kyber-format G1
//                             points (one per polynomial coefficient,
//                             threshold = f+1 entries; polyCommits[0] is
//                             clusterIBEPubKey, repeated for convenience).
//   - Encryption-at-rest reuses the storage's existing SetEncryptionKey
//     mechanism (see signer_storage.go); the IBE-share blob is treated
//     identically to wallet-account data.
//   - The "current" generation per clusterID is recorded in a sibling
//     record `signer_data-ibe_generation-` mapping clusterID → uint64,
//     written atomically alongside the share blob. Lookup by clusterID
//     resolves through this pointer.
//   - On reconfig (committee change ⇒ clusterID change), a new (clusterID,
//     generation=0) row appears; the old clusterID's row is removed once
//     the new generation is durably persisted.

// ErrIBEShareNotFound is returned by IBEShareBytesProvider methods when no
// IBE share is registered for the given clusterID.
var ErrIBEShareNotFound = errors.New("ekm: ibe share not found for cluster")

// ErrIBEShareNotImplemented is returned by signer implementations that do
// not support IBE-share storage. RemoteKeyManager returns this until the
// ssv-signer / Web3Signer drand-DST extension lands (tracked as FW1 in
// docs/TBFT-DKG-TASKS.md).
var ErrIBEShareNotImplemented = errors.New("ekm: ibe share storage not implemented for this signer")

// IBEShareBytesProvider is implemented by signers that can return the
// kyber-format BLS share bytes and per-cluster IBE pubkey material the
// TBFT-IBE tag-signing path needs. Local-mode only; RemoteKeyManager
// returns ErrIBEShareNotImplemented from each method.
type IBEShareBytesProvider interface {
	// GetIBEShareBytes returns the operator's serialized kyber scalar share
	// for the cluster's current IBE generation.
	GetIBEShareBytes(clusterID [32]byte) ([]byte, error)

	// GetClusterIBEPubKey returns the cluster's IBE master public key
	// (kyber-format G1 point bytes) for the current generation. This is
	// the trust anchor the TBFT IBE primitive encrypts to.
	GetClusterIBEPubKey(clusterID [32]byte) ([]byte, error)

	// GetClusterIBEPolyCommits returns the full polynomial-commitment array
	// for the cluster's IBE keypair (one G1 point per coefficient,
	// threshold = f+1 entries). polyCommits[0] equals GetClusterIBEPubKey.
	// Used to derive each operator's IBE-share pubkey in-protocol when
	// per-NR-partial verification is enabled (see Phase E5).
	GetClusterIBEPolyCommits(clusterID [32]byte) ([][]byte, error)
}

// IBEShareWriter is implemented by signers that can persist IBE share
// material produced by a successful DKG ceremony. Single atomic write at
// FinishPhase per docs/TBFT-DKG-TASKS.md D9; mid-DKG state is intentionally
// not persisted.
type IBEShareWriter interface {
	// AddIBEShare persists the share material for (clusterID, generation).
	// Implementations write atomically: either both the share blob and the
	// generation pointer land, or neither does.
	//
	//   - shareBytes        : kyber-format scalar (operator's share).
	//   - clusterIBEPubKey  : kyber-format G1 point (master IBE pubkey).
	//   - polyCommits       : kyber-format G1 points, one per polynomial
	//                         coefficient. polyCommits[0] must equal
	//                         clusterIBEPubKey.
	AddIBEShare(
		clusterID [32]byte,
		generation uint64,
		shareBytes []byte,
		clusterIBEPubKey []byte,
		polyCommits [][]byte,
	) error

	// RemoveIBEShare deletes any persisted share material for clusterID
	// (across all generations). Idempotent — a clusterID with no record
	// is a no-op.
	RemoveIBEShare(clusterID [32]byte) error
}
