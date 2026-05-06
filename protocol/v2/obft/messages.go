package obft

// Wire-shaped message types carried between operators in the four OBFT
// envelope kinds (Phase1Bundle, KindOnion, KindNR, KindCertificate).
//
// Sender authentication is provided by the outer SignedSSVMessage envelope —
// the OperatorID fields below are claimed-by-sender values that the outer
// layer's signature verification ties to the actual operator's identity key
// (verifying the outer signature against the pubkey for OperatorIDs[0]
// effectively binds the inner OperatorID claim). The Instance methods that
// observe peer messages assume callers have already done that outer-layer
// verification; defense-in-depth checks (claim vs verified senderID) live
// at the Instance API boundary.

// Phase1Bundle is the Phase-1 message a layer's leader sends to distribute
// their fetched candidate value plus their σ partial sig on it.
//
// Per spec §Phase 1, the bundle gives the cluster a head-start of one real
// threshold partial on V_{L_k} as soon as Phase 1 succeeds anywhere — the
// leader's σ_{L_k}^V counts toward the σ-pool at L_k together with non-leader
// Phase-2 onion contributions.
type Phase1Bundle struct {
	// OperatorID is the layer's leader (claimed; outer-layer sig verifies).
	OperatorID OperatorID
	// Height is the consensus-instance identifier (slot, in SSV).
	Height Height
	// Layer is the layer index this bundle is for. The leader at layer k
	// is determined by the cluster's per-slot leader rotation.
	Layer int
	// Value is the candidate the leader fetched and committed to.
	Value Value
	// SigmaV is the leader's V-keypair partial signature on Value. Counts
	// as one of qV partials needed for cluster-wide reconstruction.
	SigmaV Signature
}

// EncryptedLayer is one layer of an Onion: a candidate value plus the σ
// partial signature on it (encrypted under the chained NR-tag stack at
// layers > 0; plaintext at layer 0).
//
// Per spec §Phase 2, the chained encryption at layer k uses tags
// nr_tag_0, ..., nr_tag_{k-1} nested with nr_tag_0 outermost. Decryption
// requires NR-quorum at every prior layer (Pigeonhole 3 sealing).
//
// Value is visible at all layers (no encryption around it) so receivers can
// group partials by signed value and detect cross-onion equivocation. Only
// the partial signature is gated by IBE.
//
// An empty EncryptedLayer (zero-length Value and Ciphertext) means the
// emitting operator did not contribute at this layer — a valid encoding
// of "I am Defer-state, not σ-emitted at this layer" or "I am the layer's
// leader but my Phase-1 σ is the contribution, not the onion".
type EncryptedLayer struct {
	Value      Value
	Ciphertext []byte
}

// Onion is the σ-side wire payload (KindOnion). Carries one operator's
// per-layer σ partials (plaintext at L_0, chained-encrypted at deeper layers).
//
// Per spec §Phase 2, KindOnion may be emitted multiple times per (slot,
// operator) as σ-eligibility transitions late (e.g., late re-flood delivers
// V to a previously-Defer-state operator at layer k mid-Phase-2 → operator
// emits a fresh KindOnion reflecting the new σ-eligibility). Receivers track
// per-(operator, layer) σ-presence cumulatively across all observed Onions
// from the same operator; the "first auth-valid emission per (operator,
// layer)" wins for σ-pool / Defer-rule purposes, and a second emission with
// a distinct value at the same layer is cross-onion equivocation evidence.
type Onion struct {
	OperatorID OperatorID
	Height     Height
	// Layers has length K; layer k carries this operator's contribution at
	// layer k (or empty if the operator did not σ-emit at that layer).
	Layers []EncryptedLayer
}

// NRPartial is one operator's partial NR signature for a specific layer,
// appearing inside a KindNR alongside other layers the same operator
// committed NR-side at end-of-Phase-2.
type NRPartial struct {
	// Layer in [0, K-1) — there is no NR tag for the deepest layer
	// (no further layer to advance to).
	Layer int
	// PartialSig is the operator's IBE-keypair partial signature on
	// NoQuorumTag(ClusterID, Height, Layer). Aggregating qEnc of these
	// at the same Layer yields the chained-decryption key for nr_tag_Layer.
	PartialSig Signature
}

// NR is the NR-side wire payload (KindNR). Carries one operator's NR
// commitments across layers, all in one envelope.
//
// Per spec §Phase 2 / Wire format, KindNR is "Emitted at most once per
// operator per slot" and "Carries i's NR/NV partials for layers committed
// to NR at end-of-Phase-2 force-commit". Bundling avoids fanning out many
// envelopes at once.
//
// NR (silent-leader) and NV (host-not-valid) commitments are operationally
// identical for the protocol and both produce IBE partials on the layer's
// nr_tag — local diagnostic only distinguishes them.
type NR struct {
	OperatorID OperatorID
	Height     Height
	Partials   []NRPartial
}

// Certificate is the final-certificate wire payload (KindCertificate). Per
// spec §Final-certificate gossip, after an operator successfully reconstructs
// (V, S) it gossips this certificate so that receivers without local
// reconstruction can submit (V, S) downstream — protecting against the
// "lone-reconstructor's beacon path fails" failure mode.
type Certificate struct {
	Height Height
	Value  Value
	// Signature is the full reconstructed BLS signature on Value, verifiable
	// against the cluster's V-keypair pubkey.
	Signature Signature
}

// Output is the result of a successful consensus instance: which layer
// reached σ-quorum, what value was decided, and the reconstructed full BLS
// signature on it.
type Output struct {
	Layer     int
	Value     Value
	Signature Signature
}
