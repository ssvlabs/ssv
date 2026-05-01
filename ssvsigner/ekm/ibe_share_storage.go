package ekm

import "errors"

// ibe_share_storage.go declares the interfaces, on-disk record shape, and
// storage schema for per-cluster IBE share material used by the TBFT
// proposer-duty path under Option B. See docs/TBFT-DKG-TASKS.md (Phase D)
// for the full plan; the design stub landed in Phase A3 and the
// implementation now lives alongside.
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
// Schema:
//
//   - One BadgerDB record per clusterID, under the prefix
//     `signer_data-ibe_share-`. The value is a JSON-encoded
//     IBEShareRecord wrapped through encryptData (the same encryption
//     applied to wallet-account records — see signer_storage.go).
//   - Only the current generation is kept per cluster. On reconfig
//     (committee change ⇒ different clusterID) or re-DKG, the orchestrator
//     calls RemoveIBEShare for the old cluster after the new record has
//     durably landed.

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

	// RemoveIBEShare deletes any persisted share material for clusterID.
	// Idempotent — a clusterID with no record is a no-op.
	RemoveIBEShare(clusterID [32]byte) error
}

// IBEShareRecord is the on-disk shape of a per-cluster IBE-share entry.
// Stored as JSON inside the encrypted blob (encryption-at-rest reuses
// signer_storage.go's encryptData / decryptData wrapper).
//
// Field order is the natural reading order: identity (Generation), then
// the operator's secret share, then public material (cluster IBE pubkey
// + polynomial commitments). Generation is part of the value rather than
// the key so a successful AddIBEShare for a new generation atomically
// supersedes any prior record for the same clusterID.
type IBEShareRecord struct {
	Generation       uint64   `json:"generation"`
	ShareBytes       []byte   `json:"share_bytes"`
	ClusterIBEPubKey []byte   `json:"cluster_ibe_pubkey"`
	PolyCommits      [][]byte `json:"poly_commits"`
}
