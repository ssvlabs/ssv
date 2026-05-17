package base

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

// Wire-shaped message types carried between operators in the three OBFT
// envelope kinds (Phase1Bundle, KindCommit, KindCertificate).
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
// Phase-2 commit contributions.
type Phase1Bundle struct {
	// ClusterID identifies the cluster this bundle is for. Receivers reject
	// bundles whose ClusterID doesn't match their instance's ClusterID
	// (defense-in-depth against cross-cluster replay; the outer SSV envelope's
	// MsgID also binds cluster context, so this is belt-and-braces).
	ClusterID [32]byte
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

// EncryptedLayer is one layer of a Commit's σ-side onion: a candidate value
// plus the σ partial signature on it (encrypted under the chained NR-tag stack
// at layers > 0; plaintext at layer 0).
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
// emitting operator did not σ-emit at this layer (they emitted NR or NV at
// this layer, or it is the layer they lead and their Phase-1 σ_V is the
// contribution).
type EncryptedLayer struct {
	Value      Value
	Ciphertext []byte
}

// NRPartial is one operator's partial NR signature for a specific layer,
// appearing inside a Commit alongside other layers the same operator
// committed NR-side at T_commit.
type NRPartial struct {
	// Layer in [0, K-1) — there is no NR tag for the deepest layer
	// (no further layer to advance to).
	Layer int
	// PartialSig is the operator's IBE-keypair partial signature on
	// NoQuorumTag(ClusterID, Height, Layer). Aggregating qEnc of these
	// at the same Layer yields the chained-decryption key for nr_tag_Layer.
	PartialSig Signature
}

// LeaderSigmaWitness is a plaintext copy of the (layer, value, σ_L^V)
// triple extracted from a Phase-1 bundle the emitting operator has retained.
// Per spec §Phase 2 / Wire format and §Appendix C, every KindCommit carries
// witnesses for every Phase-1 bundle the operator has retained at T_commit.
//
// **Primary V-drop recovery path (peer-reflood V via early commit, spec
// §Phase 2):** a V-drop receiver P who hasn't received the leader's
// Phase-1 bundle but observes a peer's KindCommit can recover both V (from
// the peer's σ-side onion plaintext Value field at L_0) and σ_L^V (from
// the peer's witness section here, via ValueRoot cross-reference against
// V from the same peer's onion). Combined: P verifies σ_L^V against V,
// then σ_i^V on V themselves in their own (later) KindCommit. Closes the
// h_V=1 selective-Phase-1-delivery deadlock at L_0. See
// Instance.harvestWitness / Instance.chosenVForLayer for the impl.
//
// **Witness-alone (no V available anywhere):** unusable in isolation —
// σ_V verifies against V (via the Signer's signing target), so a receiver
// with only ValueRoot cannot reproduce the verification. The receiver
// retains the witness in case V arrives later via another peer's onion;
// if it never does, KindCertificate gossip (§Final-certificate gossip)
// is the fallback recovery — a faster peer who reconstructed (V, S)
// gossips the cert and the V-drop receiver consumes it directly.
//
// ValueRoot is sha256(V) — a wire identifier, NOT the σ_V signing target.
// (σ_V is signed via the Signer interface, which for production proposer-
// duty translates V → beacon proposer-domain signing root before signing.
// The two hashes serve different purposes: signing target = beacon-block
// signing root for downstream compatibility; wire identifier = sha256(V)
// for compact cross-referencing.)
//
// Wire savings vs shipping full V in the witness: ~10× per witness (32
// bytes vs ~1 KB at blinded-block size). V plaintext is still on-wire in
// the σ-side onion entry at L_0 (one copy per σ-emitting peer); the
// witness section adds σ_L^V without duplicating V.
//
// Current Verifier.VerifyCommitWitnesses is structural-only — full σ_V
// verification happens at the protocol layer (Instance) where retained
// V's (bundles[layer][leader] or peerOnions[layer][*].Value) are
// available for cross-reference.
type LeaderSigmaWitness struct {
	Layer     int        // layer the witnessed bundle is for; in [0, K)
	Leader    OperatorID // claimed leader (must match cfg.Layers[Layer].Leader)
	ValueRoot [32]byte   // sha256(V) — wire identifier of the V the leader signed
	SigmaV    Signature  // leader's V-keypair σ partial on V (verified against retained V at Instance layer)
}

// ValueRoot returns the 32-byte identifier (sha256) used to refer to a
// Phase-1 V on the wire (in LeaderSigmaWitness) without retransmitting
// the full bytes. Cluster-wide stable: every honest operator computes
// the same value_root for the same V.
//
// Distinct from the σ_V signing target — see LeaderSigmaWitness comment.
func ValueRoot(v Value) [32]byte {
	return sha256.Sum256(v)
}

// Commit is the wire payload carried in a single KindCommit message at
// T_commit. It bundles the operator's σ-side per-layer contributions (the
// K-layer onion: plaintext at L_0, chained-encrypted at deeper layers) plus
// their NR-side per-layer contributions (IBE partials on layers committed
// NR-side).
//
// Per spec §Phase 2, each operator emits exactly one KindCommit per (slot,
// operator) at T_commit, based on what they observed by then. Per-layer
// commitment is exclusive (an entry in Layers[k] OR an entry in NRPartials
// for layer k, never both). The Phase 2 window Δ_2 is sized to let the
// KindCommit propagate to all honest peers before Phase 3 reconstruction
// begins.
type Commit struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// Layers has length K; layer k carries this operator's σ contribution
	// at layer k (or empty if the operator did not σ-emit at that layer).
	Layers []EncryptedLayer
	// NRPartials carries this operator's IBE partials for layers committed
	// NR-side (silent-leader rule, equivocation rule, or NV).
	NRPartials []NRPartial
	// Witnesses carries plaintext (layer, value, σ_L^V) triples for every
	// Phase-1 bundle this operator has retained. Lets receivers who missed
	// the original Phase-1 broadcast still rehydrate the leader's σ
	// contribution to their local σ-pool. Per spec §Phase 2 / §Appendix C.
	Witnesses []LeaderSigmaWitness
}

// Certificate is the final-certificate wire payload (KindCertificate). Per
// spec §Final-certificate gossip, after an operator successfully reconstructs
// (V, S) it gossips this certificate so that receivers without local
// reconstruction can submit (V, S) downstream — protecting against the
// "lone-reconstructor's beacon path fails" failure mode.
type Certificate struct {
	ClusterID [32]byte
	Height    Height
	Value     Value
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

// commitContentHash returns a SHA-256 hash of c's content fields, used by
// ObserveCommit to dedup identical re-broadcasts vs flag distinct second
// emissions.
//
// NRPartials and Witnesses are hashed in canonicalized order (by Layer for
// NRPartials, by (Layer, Leader, ValueRoot) for Witnesses) so a byzantine
// can't manufacture a structurally-distinct-but-logically-identical Commit
// by reordering these slices. Layers is positional (indexed by k) so its
// natural order is already canonical. Without canonicalization, two reordered
// commits would hash to different values and fire a spurious Rule 3
// entry even though they carry the same logical content; the slashing
// record stays clean by treating reorders as the duplicate they are.
func commitContentHash(c *Commit) [32]byte {
	h := sha256.New()
	h.Write(c.ClusterID[:])
	binary.Write(h, binary.BigEndian, uint64(c.OperatorID))
	binary.Write(h, binary.BigEndian, uint64(c.Height))
	binary.Write(h, binary.BigEndian, uint32(len(c.Layers)))
	for _, el := range c.Layers {
		binary.Write(h, binary.BigEndian, uint32(len(el.Value)))
		h.Write(el.Value)
		binary.Write(h, binary.BigEndian, uint32(len(el.Ciphertext)))
		h.Write(el.Ciphertext)
	}
	nr := make([]NRPartial, len(c.NRPartials))
	copy(nr, c.NRPartials)
	sort.Slice(nr, func(i, j int) bool { return nr[i].Layer < nr[j].Layer })
	binary.Write(h, binary.BigEndian, uint32(len(nr)))
	for _, p := range nr {
		binary.Write(h, binary.BigEndian, uint32(p.Layer))
		binary.Write(h, binary.BigEndian, uint32(len(p.PartialSig)))
		h.Write(p.PartialSig)
	}
	ws := make([]LeaderSigmaWitness, len(c.Witnesses))
	copy(ws, c.Witnesses)
	sort.Slice(ws, func(i, j int) bool {
		if ws[i].Layer != ws[j].Layer {
			return ws[i].Layer < ws[j].Layer
		}
		if ws[i].Leader != ws[j].Leader {
			return ws[i].Leader < ws[j].Leader
		}
		return bytes.Compare(ws[i].ValueRoot[:], ws[j].ValueRoot[:]) < 0
	})
	binary.Write(h, binary.BigEndian, uint32(len(ws)))
	for _, w := range ws {
		binary.Write(h, binary.BigEndian, uint32(w.Layer))
		binary.Write(h, binary.BigEndian, uint64(w.Leader))
		h.Write(w.ValueRoot[:])
		binary.Write(h, binary.BigEndian, uint32(len(w.SigmaV)))
		h.Write(w.SigmaV)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
