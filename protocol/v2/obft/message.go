package obft

import "crypto/sha256"

// Shared wire types used by both OBFT protocols. base and twoab re-export
// these as type aliases (base.Certificate / twoab.Certificate, etc.), so the
// struct shapes are defined exactly once.

// Certificate is the final-certificate wire payload (KindCertificate). After
// an operator reconstructs (V, S) it gossips this certificate so receivers
// without local reconstruction can submit (V, S) downstream — protecting
// against the lone-reconstructor's beacon path failing.
type Certificate struct {
	ClusterID [32]byte
	Height    Height
	Value     Value
	// Signature is the full reconstructed BLS signature on Value, verifiable
	// against the cluster's V-keypair pubkey.
	Signature Signature
}

// Output is the result of a successful consensus instance: which layer reached
// σ-quorum, what value was decided, and the reconstructed full BLS signature
// on it.
type Output struct {
	Layer     int
	Value     Value
	Signature Signature
}

// ValueRoot returns the 32-byte identifier (sha256) used to refer to a value
// on the wire without retransmitting the full bytes. Cluster-wide stable:
// every honest operator computes the same value_root for the same V. Distinct
// from the σ_V signing target, which the Signer derives separately.
func ValueRoot(v Value) [32]byte {
	return sha256.Sum256(v)
}

// Phase1Bundle is the Phase-1 message a layer's leader broadcasts to
// distribute its fetched candidate value plus its σ partial on it. The
// partial gives the cluster a head-start of one real threshold share on Value
// at that layer as soon as Phase 1 succeeds anywhere.
//
// Authentication: the outer envelope is op-identity-signed, binding the
// claimed OperatorID to the signer. LeaderSigma adds threshold-scheme
// binding on Value — receivers verify it against pubKeyShares[OperatorID]
// on Value, so a forged Value cannot be paired with a valid LeaderSigma
// without forging the leader's BLS share.
type Phase1Bundle struct {
	ClusterID  [32]byte
	OperatorID OperatorID // the layer's leader (claimed; outer-envelope sig verifies)
	Height     Height
	Layer      int
	Value      Value // the candidate the leader fetched
	// LeaderSigma is the layer leader's σ partial (BLS threshold share) on Value,
	// verifiable against pubKeyShares[OperatorID]. Receivers pool it into
	// σ-pool[Layer][ValueRoot(Value)] on observation; a verify failure is
	// slashable (fake plaintext σ against the leader).
	LeaderSigma Signature
}
