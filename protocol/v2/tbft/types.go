// Package tbft implements the TBFT (Threshold BFT) protocol — a single-shot,
// deadline-driven agreement protocol that achieves safety cryptographically
// rather than via multi-round message exchange.
//
// The protocol is described in docs/TBFT.md. The two-layer specialization
// (TBFT2) is described in docs/TBFT2.md. Both are produced by the same code
// path: TBFT2 is just TBFT with K=2 and a per-layer fetch-time tweak.
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// All types here are generic; SSV-specific integration lives in adapter
// packages that translate between SSV types and TBFT types.
package tbft

import (
	"errors"
	"time"
)

// OperatorID identifies a participant in the cluster. Matches uint64 ID space
// used elsewhere in SSV but is not coupled to spec types.
type OperatorID uint64

// Height is a generic instance identifier — typically a slot number when used
// in SSV but otherwise opaque. The protocol does not interpret it; tag
// construction binds it into the IBE labels to prevent cross-instance replay.
type Height uint64

// Value is the candidate bytes being agreed upon (e.g. a serialized blinded
// block in the SSV proposer-duty case). The protocol treats it as opaque.
type Value []byte

// Signature is a BLS signature, either a partial (operator share) or a
// reconstructed full signature.
type Signature []byte

// LayerSpec describes one layer of the K-layer onion structure: which
// operator is the "leader" responsible for fetching/broadcasting the value
// at this layer, and when (relative to the consensus deadline) they should
// fetch.
//
// For TBFT proposer duty: all layers have the same FetchAt (e.g. T_commit − 1s).
// For TBFT2 proposer duty: layer 0 (primary) has a late FetchAt, layer 1
// (backup) has an early FetchAt (e.g. T_commit − 4s).
type LayerSpec struct {
	// Leader is the operator responsible for producing and broadcasting the
	// candidate value at this layer.
	Leader OperatorID
	// FetchAt is when the leader should produce the candidate, expressed
	// relative to the consensus instance start (the slot start in SSV).
	FetchAt time.Duration
}

// Config parameterizes one consensus instance.
//
// K = len(Layers). The protocol attempts the layers in order; layer 0 is the
// highest priority. A cluster of size n=4 typically uses K=2 (TBFT2); larger
// clusters use K = max(3, f+1).
type Config struct {
	// Height identifies this consensus instance (the slot, in SSV usage).
	Height Height

	// Layers is the priority-ordered list of K layers. Each layer's leader
	// must be a distinct operator.
	Layers []LayerSpec

	// Deadline is when consensus must finalize, expressed relative to the
	// instance start. After this point, operators commit to whatever
	// outcome they have locally.
	Deadline time.Duration

	// ClusterID uniquely identifies the cluster running this instance.
	// Used in tag construction so ciphertexts cannot be replayed across
	// clusters.
	ClusterID [32]byte

	// Operators is the full set of operators in the cluster, with the
	// byzantine bound `f`. Quorum threshold is `2f + 1`.
	Operators []OperatorID
	F         int
}

// Quorum returns the σ-quorum threshold qV = 2f + 1. Retained for
// backward compatibility with callers using the symmetric naming;
// equivalent to QV.
//
// Deprecated: use QV() for σ-quorum (positive partial-sig quorum) or
// QEnc() for the IBE-unlock threshold. The two are now distinct
// (qV = 2f+1, qEnc = f+1) per docs/TBFT.md; this alias hides the
// distinction and should not be used in new code.
func (c *Config) Quorum() int {
	return c.QV()
}

// QV returns the σ-quorum threshold (positive partial-sig quorum) used
// at the σ-pool reconstruction step in tryReconstructLayer. qV = 2f+1
// per [docs/TBFT.md].
func (c *Config) QV() int {
	return 2*c.F + 1
}

// QEnc returns the IBE-unlock threshold (NonReceipt-attestation quorum)
// used at tryDeriveNextLayerKey. qEnc = f+1 per [docs/TBFT.md] under
// Option B; under Option A (DST-trick on the validator key) the IBE
// primitive still requires 2f+1 partials, so this count is naming-only
// until the IBE keypair is established at threshold f+1 — see
// docs/TBFT-DKG-TASKS.md.
func (c *Config) QEnc() int {
	return c.F + 1
}

// K returns the fallback depth (number of layers).
func (c *Config) K() int {
	return len(c.Layers)
}

// Validate checks the config for internal consistency. Run once at consensus
// start; bad configs are programmer errors.
func (c *Config) Validate() error {
	if len(c.Layers) == 0 {
		return errors.New("tbft: config has no layers")
	}
	if c.F < 1 {
		return errors.New("tbft: byzantine bound F must be >= 1")
	}
	if len(c.Operators) < 3*c.F+1 {
		return errors.New("tbft: cluster size must be at least 3F+1")
	}
	if c.Deadline <= 0 {
		return errors.New("tbft: deadline must be positive")
	}

	// Verify operator IDs are distinct.
	members := make(map[OperatorID]bool, len(c.Operators))
	for _, op := range c.Operators {
		if members[op] {
			return errors.New("tbft: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	// Verify all layer leaders are distinct cluster members.
	seen := make(map[OperatorID]bool, len(c.Layers))
	for _, layer := range c.Layers {
		if !members[layer.Leader] {
			return errors.New("tbft: layer leader is not a cluster member")
		}
		if seen[layer.Leader] {
			return errors.New("tbft: duplicate leader across layers")
		}
		seen[layer.Leader] = true
		if layer.FetchAt >= c.Deadline {
			return errors.New("tbft: layer FetchAt must be before deadline")
		}
	}
	return nil
}

// EncryptedLayer is one layer of the onion: a (value, partial-signature)
// pair where the partial signature is encrypted under the layer's IBE tag.
//
// Layer 0 has Tag = nil (plaintext — the highest-priority layer is always
// openable). Layers 1..K-1 have Tag = NoQuorumTag(layer-1, ...), meaning
// they decrypt only when 2f+1 non-receipt attestations on the layer below
// have aggregated.
//
// Value is visible at all layers (no encryption around it). Only the
// partial signature is gated by IBE — the gate prevents the partial sig
// from being usable for reconstruction, not the value from being read.
// This lets receivers verify per-partial against the claimed value
// without unlocking the gate.
//
// An empty EncryptedLayer (zero-length Value and Ciphertext) means the
// emitting operator did not contribute at this layer (e.g. they didn't
// receive the layer's leader broadcast in time).
type EncryptedLayer struct {
	// Tag is the IBE label this layer's Ciphertext is encrypted under.
	// nil for layer 0 (Ciphertext is plaintext at layer 0).
	Tag []byte
	// Value is the candidate value the partial signature signs. Visible
	// at all layers so receivers can group partials by signed value and
	// detect leader equivocation.
	Value Value
	// Ciphertext is the encrypted partial signature on `Value`. For layer
	// 0, this holds the partial signature in the clear.
	Ciphertext []byte
}

// Onion is one operator's contribution to one consensus instance: a K-layer
// structure of partial signatures, one per priority layer. Constructed and
// gossipped during Phase 2.
type Onion struct {
	// OperatorID is the operator who produced this onion. Their identity-key
	// signature over the onion is checked at the network layer; not part
	// of this struct.
	OperatorID OperatorID
	// Height is the consensus-instance identifier this onion is for.
	Height Height
	// Layers has length K. Layer i carries this operator's partial signature
	// on the value proposed at layer i (encrypted under tag_i for i > 0).
	Layers []EncryptedLayer
}

// NonReceiptAttestation is a separately-broadcast partial signature on a
// no-quorum tag for a given layer. Aggregating 2f+1 of these yields the
// decryption key for the next layer's onion ciphertexts.
type NonReceiptAttestation struct {
	OperatorID OperatorID
	Height     Height
	// Layer is the layer index for which this operator did not receive (or
	// did not consider valid) the candidate value. The tag signed is
	// NoQuorumTag(Height, ClusterID, Layer).
	Layer int
	// PartialSig is operator's partial BLS signature on the no-quorum tag
	// for (Height, ClusterID, Layer).
	PartialSig Signature
}

// CandidateBroadcast is the Phase-1 message a layer's leader sends to
// distribute their fetched candidate value to peers. Without this,
// non-leader operators would have no candidate to sign at Phase 2 and
// the layer would silently fail.
//
// The protocol-layer effect on the receiver is that the operator calls
// Instance.ObserveCandidate(Layer, Value) on the relevant instance.
//
// CandidateBroadcast does not carry a signature — sender authentication
// is handled at the SSV-adapter / wire-envelope layer (the outer
// SignedSSVMessage signs the envelope including the candidate body).
type CandidateBroadcast struct {
	OperatorID OperatorID
	Height     Height
	Layer      int
	Value      Value
}

// Output is the result of a successful consensus instance: which layer
// reached positive quorum, what value was decided, and the reconstructed
// full BLS signature on it.
type Output struct {
	Layer     int
	Value     Value
	Signature Signature
}
