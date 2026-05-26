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

// LayerAttempt records one layer's worth of Phase-3 walk state for
// per-operator diagnostic and consensustest's bucket-3 walk-consistency
// invariant. Populated by Instance.Resolve as it iterates layers
// 0..K-1; readable post-Resolve via Instance.LastResolveLayerAttempts.
//
// Frozen at the moment the walk visited the layer — subsequent layers'
// decryption may unlock further σ partials that would have changed an
// earlier layer's outcome if re-checked. The OBFT spec § Phase 3 walk
// is monotonic (later partials cannot un-decide an earlier layer), so
// the snapshot is sufficient for "did the walk see a σ-quorum at this
// layer" without re-running.
//
// SigmaPoolSize counts the LARGEST sigGroup at this layer (the V most
// likely to reach qV cluster-wide); under leader equivocation multiple
// groups exist but at most one can reach qV per Pigeonhole 2. QV is the
// σ-quorum threshold for this instance (= 2f+1). NRPoolSize and QEnc
// have the same shape for the NR-pool side. The "Reached" booleans are
// pre-computed for downstream convenience: SigmaReached = SigmaPoolSize
// >= QV, NRReached = NRPoolSize >= QEnc.
//
// Decided is true iff the walk produced an Output at this layer
// (necessarily implies SigmaReached). The deepest layer (K-1) has no
// NR tag — NRPoolSize and QEnc are 0 / NRReached false there
// regardless of any contributions, since the protocol doesn't aggregate
// NR partials at the deepest layer (there's no L_K to advance to).
//
// V identity is intentionally NOT recorded. Pigeonhole 2 guarantees at
// most one V reaches qV cluster-wide per layer, so SigmaReached at a
// given layer unambiguously identifies the (cluster-wide unique) V
// that reached quorum there — the consumer can cross-reference against
// Outcome.DecidedValue / oo.Value without ambiguity. If a future
// protocol relaxes Pigeonhole 2 (multiple V's can reach qV at the
// same layer), this struct needs a V / ValueRoot field and bucket-3 D1
// in safety.go needs per-V trace fields to disambiguate "honest op's
// local Resolve reached qV on V_b while the cluster cert is on V_a"
// regressions. The cluster-wide check (NoOfflineDoubleV) catches the
// double-V end-state regardless.
type LayerAttempt struct {
	Layer         int
	SigmaPoolSize int
	QV            int
	SigmaReached  bool
	Decided       bool
	NRPoolSize    int
	QEnc          int
	NRReached     bool
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
