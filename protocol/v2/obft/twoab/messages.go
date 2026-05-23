package twoab

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// Wire-shaped message types carried between operators in the five 2abOBFT
// envelope kinds (KindPhase1Bundle, KindValue, KindNoValue, KindCommit,
// KindCertificate).
//
// Naming note: the protocol-message struct for KindValue is `ValueMsg` (not
// `Value`) because `Value` is already a type alias for `obft.Value` (the
// candidate-value `[]byte`). For symmetry with `ValueMsg`, the KindNoValue
// struct is `NoValueMsg`. `Commit` is unambiguous and follows the bare-OBFT
// `base/messages.go` naming convention.
//
// Sender authentication is provided by the outer SignedSSVMessage envelope
// in production (the same model as bare OBFT). The OperatorID fields below
// are claimed-by-sender values that the outer layer's signature verification
// ties to the actual operator's identity key.

// LayerEntryKind discriminates the per-layer commitment direction carried
// inside Phase-2a emissions' LayerEntries (one entry per layer k ∈ [1, K-1]):
//
//   - LayerEntryEmpty: the operator does not commit to either side at this
//     layer. Used at the deepest layer L_{K-1} when the operator is NR-side
//     (no nr_tag_{K-1} exists), and as a defensive default.
//   - LayerEntrySigmaChained: the operator commits σ-direction on V at this
//     layer. Payload is the σ partial, chained-IBE-encrypted under
//     nr_tag_0..nr_tag_{k-1} (k levels of encryption). V field is set.
//   - LayerEntryNRPlaintext: the operator commits NR-direction at this layer.
//     Payload is the plaintext nr_tag_k IBE partial. V field is empty. Only
//     valid for k ∈ [0, K-2] — there is no nr_tag at the deepest layer.
type LayerEntryKind byte

const (
	LayerEntryEmpty        LayerEntryKind = 0x00
	LayerEntrySigmaChained LayerEntryKind = 0x01
	LayerEntryNRPlaintext  LayerEntryKind = 0x02
)

// String returns a human-readable label for telemetry/logging.
func (k LayerEntryKind) String() string {
	switch k {
	case LayerEntryEmpty:
		return "empty"
	case LayerEntrySigmaChained:
		return "sigma-chained"
	case LayerEntryNRPlaintext:
		return "nr-plaintext"
	default:
		return "unspecified"
	}
}

// LayerEntry is one operator's per-layer commitment carried inside a
// Phase-2a emission (ValueMsg, NoValueMsg, or Commit with Side=NRDirect).
// Each Phase-2a emission carries K-1 entries: one for each layer k ∈ [1, K-1].
//
// L_0 is NOT carried in LayerEntries — the L_0 commitment is in the wire-
// envelope kind itself (ValueMsg ⇒ σ-direction-claim with V_0 fulltext;
// NoValueMsg ⇒ NR-direction-claim or pending; Commit at Phase 2b carries
// the actual L_0 partial).
//
// Per spec §Wire format, the encoder represents Empty entries with a kind
// byte only (no payload); SigmaChained carries V + chained ciphertext;
// NRPlaintext carries the IBE partial bytes only (no V).
type LayerEntry struct {
	// Layer is the layer index this entry corresponds to, k ∈ [1, K-1].
	// Carried explicitly so receivers can detect missing / duplicate layers
	// even under malformed encodings; structural validation rejects out-of-
	// range or duplicate-layer entries.
	Layer int

	// Kind discriminates the entry shape.
	Kind LayerEntryKind

	// V is the candidate value at this layer for SigmaChained entries;
	// empty for Empty and NRPlaintext.
	V Value

	// Payload is:
	//   - empty for Empty
	//   - the chained-IBE-encrypted σ partial (encrypted under nr_tag_0
	//     ... nr_tag_{k-1}, k levels) for SigmaChained
	//   - the plaintext nr_tag_k IBE partial for NRPlaintext
	Payload []byte
}

// CommitSide discriminates the L_0 direction of a Phase-2b Commit emission.
// Phase 2a emissions: ValueMsg carries the emitter's σ partial directly at
// L_0 (post Op5 — see ValueMsg.L0Partial); NoValueMsg carries no L_0
// partial. Commit at Phase 2b carries only the NR-direction partial in one
// of two sides:
//
//   - CommitSideNR: NR-direction at L_0 (Phase-2b emission). Carries the
//     plaintext nr_tag_0 IBE partial in L0Partial.
//     Emitted after a prior KindNoValue when the NR-eligibility trigger
//     fires (per A5 authorized pair).
//   - CommitSideNRDirect: NR-direction at L_0 (Phase-2a emission,
//     equivocation observed pre-emission). Same L_0 shape as CommitSideNR,
//     but additionally carries the K-1 LayerEntries since the op skips
//     ValueMsg/NoValueMsg entirely (per A8 authorized pair).
//
// Post Op5 the wire kind set has no "Signed" side — KindValue itself IS
// the σ-side terminal emission at L_0. The pre-Op5 CommitSideSigned
// constant was removed; receivers MUST reject any commit with that
// historical value at validation time.
//
// CommitSideNR at Phase 2b references the op's earlier KindNoValue for
// the L_k>0 entries (already on the wire). CommitSideNRDirect carries its
// own L_k>0 entries.
type CommitSide byte

const (
	// CommitSideUnspecified is the zero value; never valid on the wire.
	CommitSideUnspecified CommitSide = 0x00
	// 0x01 was the pre-Op5 CommitSideSigned constant. Removed: KindValue
	// now carries the σ partial directly. Decoders MUST reject this value
	// to surface cluster-wide wire-version drift loudly. Reservation
	// preserved (do not reuse 0x01 for a new constant).
	CommitSideNR       CommitSide = 0x02
	CommitSideNRDirect CommitSide = 0x03
)

// String returns a human-readable label for telemetry/logging.
func (s CommitSide) String() string {
	switch s {
	case CommitSideNR:
		return "nr"
	case CommitSideNRDirect:
		return "nr-direct"
	default:
		return "unspecified"
	}
}

// IsNR reports whether the commit side is NR-direction (either Phase-2b NR
// or Phase-2a NRDirect). Post Op5, all valid Commit sides ARE NR-direction
// — but the method is kept for call-site clarity vs IsNRDirect.
func (s CommitSide) IsNR() bool {
	return s == CommitSideNR || s == CommitSideNRDirect
}

// Phase1Bundle is the Phase-1 message a layer's leader sends to distribute
// their fetched candidate value. Per spec §Phase 1: the bundle pairs the
// candidate value with the layer leader's σ partial on V at this bundle's
// layer (the `LWitness` field below). Wire-level leader-binding closes the
// Variant-A withhold-then-fake-σ attack and seeds σ-pool[V_k] (k = Layer)
// with one partial from the moment the bundle is observed, accelerating
// cluster-wide σ-quorum aggregation.
//
// Per-layer witness (Op8): pre-Op8 only the L_0 leader carried a witness
// (then named L0Witness). Post-Op8 every layer's leader carries LWitness =
// its σ partial on V at that layer, so σ-pool[V_k] gets a leader head-start
// at every fall-through layer (parity with bare OBFT's per-layer witness
// section). The deeper-layer witnesses are plaintext head-starts (1 < qV,
// so a witness alone never reaches σ-quorum — Pigeonhole 1 holds).
//
// Authentication: the outer envelope is op-identity-signed at construction
// time; the inner bundle bytes (encoded by EncodePhase1Bundle) are what the
// signature covers, so the (claimed) OperatorID below is bound to the
// signer's identity at the envelope layer. LWitness adds threshold-scheme
// binding on V: receivers verify LWitness against the leader's pubKeyShare
// on V, so a byz operator-identity-forging V_fake cannot pair it with a
// valid LWitness without forging the leader's BLS share.
type Phase1Bundle struct {
	// ClusterID identifies the cluster this bundle is for. Receivers reject
	// bundles whose ClusterID doesn't match their instance's ClusterID.
	ClusterID [32]byte
	// OperatorID is the layer's leader (claimed; outer-envelope sig verifies).
	OperatorID OperatorID
	// Height is the consensus-instance identifier (slot, in SSV).
	Height Height
	// Layer is the layer index this bundle is for.
	Layer int
	// Value is the candidate the leader fetched and committed to.
	Value Value
	// LWitness is the layer leader's σ partial on Value at this bundle's
	// Layer (BLS threshold share signature, verifiable against
	// pubKeyShares[OperatorID] on Value). Required and non-empty at every
	// layer (Op8). Receivers verify on observation and pool into
	// σ-pool[Layer][ValueRoot(Value)] on success; verification failure
	// fires Rule 5 (fake plaintext σ) against the leader.
	//
	// Per spec §Phase 1: the witness is structural — it cryptographically
	// binds V to the leader at the protocol layer, restoring the binding
	// that the v4 first-pass deferred via outer-envelope-only auth (per
	// §Implementation deviations #1, superseded by this design).
	LWitness Signature
}

// LayerWitness is a forwarded leader σ-witness carried in a KindValue's
// Witnesses section (Op12). It re-broadcasts a layer leader's σ partial on
// V at `Layer` so peers who missed that layer's Phase-1 bundle can still
// seed σ-pool[V_Layer][leader] with the leader's head-start partial.
//
// Wire shape is root-only (no full V) for compactness (~32 B vs ~1 KB):
// the V bytes the receiver needs to BLS-verify Witness ride alongside in
// the SAME KindValue — at Layer 0 the value is the KindValue's own V; at
// Layer k>0 it is the V in this op's SigmaChained LayerEntry at that layer.
// An emitter therefore forwards a witness only for layers it is σ-side on
// (where that colocated V is present), per the Op12 "root + σ-colocation"
// design (docs/2abOBFT.md §Phase 2a, Forwarded leader witnesses). Verification +
// retention happens at the Instance layer (ObserveValueMsg harvest), where
// Witness is checked against pubKeyShares[layer-leader] on the colocated V.
type LayerWitness struct {
	// Layer is the layer the witnessed bundle is for, in [0, K).
	Layer int
	// ValueRoot is sha256(V) — matches the colocated V (the KindValue's V
	// at Layer 0, or the SigmaChained LayerEntry's V at Layer k>0).
	ValueRoot [32]byte
	// Witness is the layer leader's σ partial on V (BLS threshold share),
	// verifiable against pubKeyShares[cfg.Layers[Layer].Leader] on V.
	Witness Signature
}

// ValueMsg is the σ-direction Phase-2 emission at L_0. Post Op5, it is
// the TERMINAL σ-side emission at L_0 — KindValue itself carries the
// emitter's plaintext σ partial on V (the L0Partial field). The pre-Op5
// two-hop cascade `KindValue → KindCommit-Signed` collapses to a single
// hop: receivers observing KindValue immediately verify+pool the σ
// partial into σ-pool[V_0_root], and the cluster reaches σ-quorum in 1·BTT
// post-emission instead of 2·BTT.
//
// Emission paths:
//   - Phase-2a fire-time (V retained + host valid at TPhase2a) → KindValue
//     emitted directly with σ partial signed at emit time. EKM acquires
//     σ-lock at L_0 on V at this moment (was at Phase-2b Commit-Signed
//     pre-Op5).
//   - A1 upgrade path (NoValueMsg-path op later receives V + host valid)
//     → emit upgrade KindValue with same wire shape. The upgrade KindValue
//     IS the σ-commit; there is no follow-up Commit-Signed under Op5.
//
// **V binding to leader (post Op11 + Op12)**: the Witnesses field carries
// forwarded leader σ-witnesses, one per layer the emitter is σ-side on.
// The Layer-0 entry is the L_0 leader's σ partial on this KindValue's V
// (the Op11 peer-reflood-V vector); deeper-layer entries (Op12) carry the
// L_k leader's σ partial on V_k, with V_k riding in this op's SigmaChained
// LayerEntry at that layer (root + σ-colocation — see LayerWitness).
// Receivers verify each witness against the layer-leader's pubKeyShare on
// the colocated V; on success V is harvested into retention as if the
// receiver had observed that leader's Phase-1 bundle directly (closing the
// v4 first-pass §Implementation deviation #2 and enabling peer-reflood-V
// as the HV1SelectiveDelivery recovery vector — now at every fall-through
// layer). On verify failure that witness is silently dropped (anti-leader-
// framing — the emitter's envelope doesn't cryptographically attribute the
// bytes to the leader, so Rule 5 against the leader would be unsound). The
// leader-signed-garbage attack stays covered by Op3/Op8 in
// ObservePhase1Bundle, where the bundle's outer envelope binds the bytes
// to the leader.
//
// **L0Partial verify failure (post Op5)** fires Rule 5 against the
// EMITTER (not the leader): the L0Partial is the emitter's OWN signing
// artifact, verifiable against pubKeyShares[v.OperatorID] on v.V. A
// failed verify is unambiguous emitter misbehavior — by BLS-share
// unforgeability the emitter either presented random bytes or
// deliberately signed garbage. No framing-attack symmetry with the
// forwarded Witnesses.
type ValueMsg struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// V is the candidate value the operator claims σ-direction on at L_0.
	V Value
	// ValueRoot is sha256(V), included for receiver-side caching/dedup.
	ValueRoot [32]byte
	// Witnesses carries forwarded leader σ-witnesses (Op12): one LayerWitness
	// per layer this emitter is σ-side on. The Layer-0 entry (always present
	// for a well-formed KindValue — KindValue IS the L_0 σ-side emission) is
	// the L_0 leader's σ partial on V, byte-for-byte forwarded from the
	// emitter's retained Phase-1 bundle. Deeper-layer entries carry the L_k
	// leader's σ partial on V_k (V_k colocated in the SigmaChained LayerEntry
	// at that layer). Receivers verify each against pubKeyShares[layer-leader]
	// on the colocated V and harvest V into retainedBundles[layer][leader]
	// (peer-reflood-V) + seed σ-pool[layer][V_root][leader]. On verify
	// failure: that entry is silently discarded (anti-framing — the emitter's
	// envelope doesn't attribute the bytes to the leader).
	//
	// Honest emitters always include at least the Layer-0 witness. Byz
	// emitters who present random bytes lose no semantic advantage: the
	// harvest fails verify, no pool/retention update happens; the bogus
	// claim is wire noise.
	Witnesses []LayerWitness
	// L0Partial is the emitter's own σ partial on V at L_0 (BLS threshold
	// share signature, verifiable against pubKeyShares[OperatorID] on V).
	// Required and non-empty post Op5. Receivers verify on observation
	// and pool into σ-pool[L_0][ValueRoot(V)][emitter] on success; verify
	// failure fires Rule 5 (fake plaintext σ) against the emitter (NOT
	// the leader — see Witnesses vs L0Partial attribution distinction in
	// the type-level docstring above).
	//
	// EKM lock at L_0 is acquired when L0Partial is signed (at KindValue
	// emit time). This is ~1·BTT earlier in the slot than the v4 lock-
	// acquisition point (Phase-2b Commit-Signed). The structural cost is
	// loss of the A3/A4 pivot — an op who emits KindValue with σ partial
	// cannot pivot to KindCommit-NR mid-slot. See plan §Op5.
	L0Partial Signature
	// LayerEntries carries the operator's L_1..L_{K-1} per-layer
	// commitments. Length K-1; index 0 → layer 1, ..., index K-2 → layer K-1.
	// Each entry is one of {Empty, SigmaChained, NRPlaintext}.
	LayerEntries []LayerEntry
}

// NoValueMsg is the Phase-2a coordination envelope for an operator who
// either does not have V_0 retained OR has V_0 but host says not-valid at
// the Phase-2a fire-instant. Carries K-1 LayerEntries; no L_0 payload.
//
// NoValueMsg envelopes contribute to novalue_pool[L_0]. Per spec §Pool
// aggregation rules, NoValueMsg membership is provisional — if the same
// op later emits a ValueMsg upgrade (A1 sequence), the receiver moves
// that op from novalue_pool to value_pool.
type NoValueMsg struct {
	ClusterID    [32]byte
	OperatorID   OperatorID
	Height       Height
	LayerEntries []LayerEntry
}

// Commit is the Phase-2b binding envelope. Post Op5, Commit is NR-side
// only — KindValue carries the σ partial directly at Phase 2a, so
// Commit-Signed no longer exists. Each operator emits at most one Commit
// per (slot); at L_k>0 the per-layer commitment is already on the wire
// from Phase 2a (in ValueMsg/NoValueMsg/Commit-NRDirect LayerEntries).
//
// Per spec §Wire format (post Op5):
//
//   - Side=NR: plaintext nr_tag_0 IBE partial at L_0 carried in L0Partial.
//     LayerEntries is empty (Phase 2a emission carried the L_k>0 entries;
//     this is a Phase-2b NR commit following an earlier KindNoValue per A5).
//   - Side=NRDirect: same L_0 shape as NR, but additionally carries the
//     K-1 LayerEntries since the operator skipped ValueMsg/NoValueMsg at
//     Phase 2a (equivocation observed pre-emission, per A8). This is the
//     only Commit kind that carries LayerEntries.
//
// EKM enforces single-σ-V at L_0 (only one V can have σ partials cluster-
// wide per Pigeonhole 2) and σ-XOR-NR per (slot, layer) at the V-share /
// IBE-share level. Post Op5 the σ-lock is acquired at KindValue emit time
// (was at Commit-Signed pre-Op5); a bug that requested σ-then-NR or
// NR-then-σ at the same layer is caught by transitionToSigma /
// transitionToNR in the Instance before any wire bytes leave the build
// path.
type Commit struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// Side discriminates the L_0 commitment shape. Post Op5, valid values
	// are NR / NRDirect only — Signed is removed.
	Side CommitSide
	// L0Partial is the L_0 nr_tag_0 IBE partial (for both Side=NR and
	// Side=NRDirect).
	L0Partial Signature
	// LayerEntries carries L_1..L_{K-1} per-layer commitments. Populated
	// only for Side=NRDirect (Phase-2a NR-direct emitter who skipped
	// ValueMsg/NoValueMsg); empty for Side=NR (which references the op's
	// earlier KindNoValue for L_k>0 entries).
	LayerEntries []LayerEntry
}

// Certificate is the final-certificate wire payload (KindCertificate). Per
// spec §Final-certificate gossip, after an operator successfully
// reconstructs (V, S) it gossips this certificate so that receivers without
// local reconstruction can submit (V, S) downstream.
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

// ValueRoot returns the 32-byte identifier (sha256) used to refer to a
// Phase-1 V on the wire without retransmitting the full bytes. Cluster-
// wide stable: every honest operator computes the same value_root for the
// same V.
func ValueRoot(v Value) [32]byte {
	return sha256.Sum256(v)
}

// valueMsgContentHash returns a SHA-256 hash of v's content fields. Used
// by ObserveValue to dedup identical re-broadcasts vs flag distinct second
// emissions (Phase-2 equivocation evidence).
//
// Witnesses and L0Partial are intentionally NOT in the hash: Rule 6a is
// V-level equivocation (cross-V claims from same op), not partial-level.
// A byz emitter who sends `{V_a, real_partial}` then `{V_a, fake_partial}`
// is presenting wire noise on the same V claim — the partial-level byz
// behavior is independently handled by ObserveValueMsg's verify paths
// (fake L0Partial fires Rule 5 against the emitter; a fake forwarded
// witness silently discards per anti-framing). Including these fields in the
// dedup hash would trip Rule 6a as a false positive on byz partial
// mutation across re-broadcasts.
func valueMsgContentHash(v *ValueMsg) [32]byte {
	h := sha256.New()
	h.Write(v.ClusterID[:])
	writeUint64(h, uint64(v.OperatorID))
	writeUint64(h, uint64(v.Height))
	writeUint32(h, uint32(len(v.V)))
	h.Write(v.V)
	h.Write(v.ValueRoot[:])
	hashLayerEntries(h, v.LayerEntries)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// noValueMsgContentHash returns a SHA-256 hash of nv's content fields.
// Used by ObserveNoValue to dedup identical re-broadcasts vs flag distinct
// second emissions.
func noValueMsgContentHash(nv *NoValueMsg) [32]byte {
	h := sha256.New()
	h.Write(nv.ClusterID[:])
	writeUint64(h, uint64(nv.OperatorID))
	writeUint64(h, uint64(nv.Height))
	hashLayerEntries(h, nv.LayerEntries)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// commitContentHash returns a SHA-256 hash of c's content fields. Used by
// ObserveCommit to dedup identical re-broadcasts vs flag distinct second
// emissions (cross-side / cross-V equivocation evidence).
func commitContentHash(c *Commit) [32]byte {
	h := sha256.New()
	h.Write(c.ClusterID[:])
	writeUint64(h, uint64(c.OperatorID))
	writeUint64(h, uint64(c.Height))
	h.Write([]byte{byte(c.Side)})
	writeUint32(h, uint32(len(c.L0Partial)))
	h.Write(c.L0Partial)
	hashLayerEntries(h, c.LayerEntries)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// layerEntriesEqual reports whether two LayerEntries slices have
// byte-identical content. Used by ObserveValueMsg / ObserveNoValueMsg
// to enforce the spec's "L_k>0 entries identical across the
// KindNoValue → upgrade KindValue pair" requirement
// (§Phase 2a A1 upgrade in docs/2abOBFT.md).
// Mismatched entries from same op across the pair are Rule 6a-slashable.
func layerEntriesEqual(a, b []LayerEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Layer != b[i].Layer {
			return false
		}
		if a[i].Kind != b[i].Kind {
			return false
		}
		if !bytes.Equal(a[i].V, b[i].V) {
			return false
		}
		if !bytes.Equal(a[i].Payload, b[i].Payload) {
			return false
		}
	}
	return true
}

// hashLayerEntries appends LayerEntries content to a hasher. Entries are
// hashed in the order they appear on the wire — validation rejects out-of-
// range or duplicate-layer entries, so honest emissions produce a canonical
// ordering and identical re-broadcasts hash identically.
func hashLayerEntries(h hash.Hash, entries []LayerEntry) {
	writeUint32(h, uint32(len(entries)))
	for _, e := range entries {
		writeUint32(h, uint32(e.Layer))
		h.Write([]byte{byte(e.Kind)})
		writeUint32(h, uint32(len(e.V)))
		h.Write(e.V)
		writeUint32(h, uint32(len(e.Payload)))
		h.Write(e.Payload)
	}
}

// writeUint32 / writeUint64 append a big-endian fixed-width integer to
// a hash.Hash. These helpers replace `binary.Write(h, ...)` to avoid
// the brittle discarded-error pattern — hash.Hash.Write is documented
// to never return an error, but the discarded error from binary.Write
// is a lint magnet. Using explicit byte slicing makes the intent
// obvious and removes the error return entirely.
func writeUint32(h hash.Hash, v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	h.Write(buf[:])
}

func writeUint64(h hash.Hash, v uint64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	h.Write(buf[:])
}
