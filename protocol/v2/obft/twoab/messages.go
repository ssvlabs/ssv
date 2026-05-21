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
// Phase 2a emissions (ValueMsg / NoValueMsg) carry no L_0 partial — they
// are op-identity-signed coordination only. Commit at Phase 2b carries the
// L_0 threshold partial in one of three sides:
//
//   - CommitSideSigned: σ-direction at L_0. Carries plaintext σ partial on
//     V_0 (the L0Partial field) plus the L0Value (the V_0 being signed).
//   - CommitSideNR: NR-direction at L_0 (Phase-2b emission). Carries the
//     plaintext nr_tag_0 IBE partial in L0Partial; L0Value is empty.
//   - CommitSideNRDirect: NR-direction at L_0 (Phase-2a emission, equivocation
//     observed). Same wire shape as CommitSideNR for L_0, but additionally
//     carries the full K-1 LayerEntries set (since the op skips ValueMsg /
//     NoValueMsg entirely and the L_k>0 entries must travel with this
//     emission to reach Phase-3 reconstruction).
//
// CommitSideSigned and CommitSideNR emissions at Phase 2b reference the op's
// earlier ValueMsg / NoValueMsg for the L_k>0 partials (already on the
// wire from Phase 2a). CommitSideNRDirect carries its own L_k>0 entries.
type CommitSide byte

const (
	// CommitSideUnspecified is the zero value; never valid on the wire.
	CommitSideUnspecified CommitSide = 0x00
	CommitSideSigned      CommitSide = 0x01
	CommitSideNR          CommitSide = 0x02
	CommitSideNRDirect    CommitSide = 0x03
)

// String returns a human-readable label for telemetry/logging.
func (s CommitSide) String() string {
	switch s {
	case CommitSideSigned:
		return "signed"
	case CommitSideNR:
		return "nr"
	case CommitSideNRDirect:
		return "nr-direct"
	default:
		return "unspecified"
	}
}

// IsNR reports whether the commit side is NR-direction (either Phase-2b NR
// or Phase-2a NRDirect). σ-XOR-NR per layer at L_0.
func (s CommitSide) IsNR() bool {
	return s == CommitSideNR || s == CommitSideNRDirect
}

// Phase1Bundle is the Phase-1 message a layer's leader sends to distribute
// their fetched candidate value. Per spec §Phase 1: 2abOBFT removes the
// Phase-1 σ_V partial entirely (leader emits σ at Phase 2b uniformly with
// all other operators), so the bundle carries only the value and
// authentication context.
//
// Authentication: the outer envelope is op-identity-signed at construction
// time; the inner bundle bytes (encoded by EncodePhase1Bundle) are what the
// signature covers, so the (claimed) OperatorID below is bound to the
// signer's identity at the envelope layer.
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
}

// ValueMsg is the Phase-2a coordination envelope for an operator who has
// V_0 retained AND host re-validates V_0 as valid at the Phase-2a fire-
// instant. Carries V_0 (fulltext) plus K-1 LayerEntries for the deeper
// layers.
//
// Per spec §Phase 2a: ValueMsg envelopes are op-identity-signed at the
// wire layer (NOT threshold partials at L_0 — they only carry a
// σ-direction-claim at L_0). They contribute to value_pool[V_0] in
// receivers' views; receivers use the inference rules in §Pool aggregation
// rules to combine ValueMsg / NoValueMsg / Commit observations into the
// cluster-wide pool view.
//
// A ValueMsg emission also doubles as the upgrade path A1: a NoValueMsg-
// path op who later receives V_0 + host valid emits a ValueMsg envelope
// with the same wire shape (per spec §Authorized Phase-2 emission pairs A1).
//
// **V_0 binding to leader**: the redesign-plan earlier had a leader-auth-sig
// field embedded in ValueMsg, intended to let receivers verify that V_0
// came from the layer's leader before propagating it as retention (the
// "peer-reflood-V" path). That field is not in this implementation — the
// leader-binding closure is achieved via:
//
//  1. The outer SignedSSVMessage envelope, which op-identity-signs the
//     broadcaster (not the leader directly).
//  2. The upgrade path's requirement that V_0 must be in
//     retainedBundles[layer][leaderID] — populated only by
//     ObservePhase1Bundle, which validates the bundle's leader-auth at
//     ValidatePhase1Bundle structural-shape time and at the outer
//     envelope-signature verification done by the SSV adapter before
//     reaching this package.
//
// Consequence: receivers do NOT propagate V_0 from observed ValueMsg into
// retention (the "peer-reflood-V via KindValue" propagation vector in the
// plan's worked cases). V_0 retention enters only via Phase-1 bundle
// gossipsub reflood. Byz operators can spam ValueMsg envelopes claiming
// arbitrary fake V_0' values, inflating value_pool[V_0'_fake] membership
// claims, but honest receivers won't have V_0'_fake retained — so their
// commit-side decision never produces a σ partial on V_0'_fake, and the
// σ-pool semantics (threshold partials only, not inferred claims) keep
// the cluster from converging on fake V's. At f=1 n=4 the byz alone
// can't push value_pool[V_0'_fake] past qV=3.
type ValueMsg struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// V is the candidate value the operator claims σ-direction on at L_0.
	V Value
	// ValueRoot is sha256(V), included for receiver-side caching/dedup.
	ValueRoot [32]byte
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

// Commit is the Phase-2b binding envelope. Each operator emits at most one
// Commit per (slot) — the Side flag distinguishes the L_0 σ vs NR direction;
// at L_k>0 the per-layer commitment is already on the wire from Phase 2a
// (in ValueMsg/NoValueMsg/Commit-NRDirect LayerEntries).
//
// Per spec §Wire format:
//
//   - Side=Signed: plaintext σ partial on V_0 at L_0. L0Value carries V_0;
//     L0Partial carries the σ partial. LayerEntries is empty (Phase 2a
//     emission carried the L_k>0 σ-chained entries).
//   - Side=NR: plaintext nr_tag_0 IBE partial at L_0. L0Value is empty;
//     L0Partial carries the partial. LayerEntries is empty (Phase 2a
//     emission carried the L_k>0 entries; this is a Phase-2b NR commit
//     following an earlier ValueMsg or NoValueMsg from the same op).
//   - Side=NRDirect: same L_0 shape as NR (nr_tag_0 partial in L0Partial),
//     but additionally carries the K-1 LayerEntries since the operator
//     skipped ValueMsg/NoValueMsg at Phase 2a (equivocation observed).
//     This is the only Commit kind that carries LayerEntries.
//
// EKM enforces single-σ-V at L_0 (only one V can have σ partials cluster-
// wide per Pigeonhole 2) and σ-XOR-NR per (slot, layer) at the V-share /
// IBE-share level. A bug that requested σ-then-NR at the same layer is
// caught by transitionToSigma / transitionToNR in the Instance before any
// wire bytes leave the build path.
type Commit struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// Side discriminates the L_0 commitment shape.
	Side CommitSide
	// L0Value is the V_0 being σ-signed (Side=Signed only); empty for
	// NR / NRDirect.
	L0Value Value
	// L0Partial is the L_0 threshold partial:
	//   - Side=Signed: plaintext σ partial on L0Value
	//   - Side=NR / NRDirect: plaintext nr_tag_0 IBE partial
	L0Partial Signature
	// LayerEntries carries L_1..L_{K-1} per-layer commitments. Populated
	// only for Side=NRDirect (Phase-2a NR-direct emitter who skipped
	// ValueMsg/NoValueMsg); empty for Side=Signed and Side=NR (those
	// reference the op's earlier Phase-2a emission for L_k>0 entries).
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
	writeUint32(h, uint32(len(c.L0Value)))
	h.Write(c.L0Value)
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
// (§Phase 2a-late upgrade in docs/2abOBFT-REDESIGN-PLAN.md).
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
