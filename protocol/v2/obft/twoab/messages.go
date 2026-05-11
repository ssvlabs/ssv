package twoab

import (
	"crypto/sha256"
	"encoding/binary"
)

// Wire-shaped message types carried between operators in the four 2abOBFT
// envelope kinds (KindPhase1Bundle, KindVerdict, KindOnion2b,
// KindCertificate).
//
// Sender authentication is provided by the outer SignedSSVMessage envelope
// in production (the same model as bare OBFT). The OperatorID fields below
// are claimed-by-sender values that the outer layer's signature verification
// ties to the actual operator's identity key.

// VerdictKind discriminates the three possible Phase-2a verdicts an
// operator can broadcast per spec §Phase 2a / "Compute local verdict at
// layer k":
//
//   - σV: operator retained exactly 1 V at this layer and the host returned
//     valid at verdict-broadcast time. Counts toward verdict_pool[V].
//   - NV: operator retained exactly 1 V but the host returned not-valid.
//     Counts toward nr_pool.
//   - NR: operator retained 0 V's OR ≥ 2 distinct V's (equivocation observed).
//     Counts toward nr_pool. (Wire-identical to NV; the distinction is
//     local-diagnostic only — see spec §Operator commitment states.)
type VerdictKind byte

const (
	// VerdictUnspecified is the zero value; never valid on the wire.
	VerdictUnspecified VerdictKind = 0x00
	// VerdictSigmaV means the operator declares σ-eligibility on a specific V.
	VerdictSigmaV VerdictKind = 0x01
	// VerdictNR means "no V retained" or "equivocation observed". value_root
	// is null (zero bytes) for both NR and NV.
	VerdictNR VerdictKind = 0x02
	// VerdictNV means "host returned not-valid on the single retained V".
	// Wire-identical to NR for convergence-rule purposes (both count toward
	// nr_pool); distinguished only for local diagnostic.
	VerdictNV VerdictKind = 0x03
)

// String returns a human-readable label for telemetry/logging.
func (k VerdictKind) String() string {
	switch k {
	case VerdictSigmaV:
		return "sigmaV"
	case VerdictNR:
		return "NR"
	case VerdictNV:
		return "NV"
	default:
		return "unspecified"
	}
}

// IsNoSigmaSide returns true for verdicts that count toward nr_pool (NR or
// NV). Convergence-rule input.
func (k VerdictKind) IsNoSigmaSide() bool {
	return k == VerdictNR || k == VerdictNV
}

// Phase1Bundle is the Phase-1 message a layer's leader sends to distribute
// their fetched candidate value. Per spec §Phase 1 Variant C: 2abOBFT
// removes the Phase-1 σ_V partial entirely (leader emits σ at Phase 2b
// uniformly with all other operators), so the bundle carries only the value
// and authentication context.
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

// Verdict is the Phase-2a verdict envelope an operator broadcasts to declare
// their σ-eligibility per layer. Per spec §Phase 2a, every operator emits
// exactly one Verdict per (slot, layer); a second distinct verdict is Rule-6a
// slashable.
//
// Verdicts are op-identity-signed at the envelope layer (NOT threshold
// partials — they bind no σ-pool / nr_pool entry directly). They only
// influence other operators' convergence-rule evaluation at Phase-2b start.
type Verdict struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	Layer      int
	Kind       VerdictKind
	// ValueRoot is sha256(V) for σV verdicts; zero ([32]byte{}) for NR / NV
	// per spec §Phase 2a (value_root = null when verdict is NR or NV).
	ValueRoot [32]byte
}

// EncryptedLayer is one layer of a Phase-2b onion's σ-side per-layer
// contribution: a candidate value plus the σ partial signature on it
// (encrypted under the chained NR-tag stack at layers > 0; plaintext at
// layer 0).
//
// Per spec §Phase 2b emission, chained encryption at layer k uses tags
// nr_tag_0, ..., nr_tag_{k-1} nested with nr_tag_0 outermost. Decryption
// requires NR-quorum at every prior layer (Pigeonhole 3 sealing).
//
// An empty EncryptedLayer (zero-length Value and Ciphertext) means the
// emitting operator did not σ-emit at this layer (they emitted NR or NV
// at this layer, or it is the deepest layer L_{K-1} where no NR tag
// exists so a "force-NR" produces no on-wire emission — per spec
// §Convergence rule "Deepest-layer NR has no on-wire emission").
type EncryptedLayer struct {
	Value      Value
	Ciphertext []byte
}

// NRPartial is one operator's partial NR signature for a specific layer.
type NRPartial struct {
	// Layer in [0, K-1) — there is no NR tag for the deepest layer.
	Layer int
	// PartialSig is the operator's IBE-keypair partial signature on
	// NoQuorumTag(ClusterID, Height, Layer). Aggregating qEnc of these
	// at the same Layer yields the chained-decryption key for nr_tag_Layer.
	PartialSig Signature
}

// Onion2b is the wire payload carried in a single KindOnion2b message at
// Phase-2b emission time. It bundles the operator's σ-side per-layer
// contributions (the K-layer onion: plaintext at L_0, chained-encrypted at
// deeper layers) plus their NR-side per-layer contributions (IBE partials
// on layers committed NR-side).
//
// Per spec §Phase 2b emission, each operator emits exactly one Onion2b per
// (slot, operator), based on their convergence-rule decision at Phase-2a
// end. Per-layer commitment is exclusive (σ entry in Layers[k] OR an entry
// in NRPartials for layer k, never both). Unlike bare-OBFT Commit, there
// is no LeaderSigmaWitness array (no Phase-1 σ_V exists in 2ab).
type Onion2b struct {
	ClusterID  [32]byte
	OperatorID OperatorID
	Height     Height
	// Layers has length K; layer k carries this operator's σ contribution
	// at layer k (or empty if the operator did not σ-emit at that layer).
	Layers []EncryptedLayer
	// NRPartials carries this operator's IBE partials for layers committed
	// NR-side. Per spec only layers in [0, K-1) have NR tags.
	NRPartials []NRPartial
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
// Phase-1 V on the wire (in Verdict envelopes) without retransmitting the
// full bytes. Cluster-wide stable: every honest operator computes the same
// value_root for the same V.
func ValueRoot(v Value) [32]byte {
	return sha256.Sum256(v)
}

// verdictContentHash returns a SHA-256 hash of v's content fields. Used by
// ObserveVerdict (Phase F) to dedup identical re-broadcasts vs flag distinct
// second emissions (Rule 6a — verdict-vs-verdict equivocation).
func verdictContentHash(v *Verdict) [32]byte { //nolint:unused // used from Phase F onward
	h := sha256.New()
	h.Write(v.ClusterID[:])
	binary.Write(h, binary.BigEndian, uint64(v.OperatorID))
	binary.Write(h, binary.BigEndian, uint64(v.Height))
	binary.Write(h, binary.BigEndian, uint32(v.Layer))
	h.Write([]byte{byte(v.Kind)})
	h.Write(v.ValueRoot[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// onion2bContentHash returns a SHA-256 hash of o's content fields. Used by
// ObserveOnion2b (Phase G) to dedup identical re-broadcasts vs flag distinct
// second emissions (cross-onion equivocation evidence).
func onion2bContentHash(o *Onion2b) [32]byte { //nolint:unused // used from Phase G onward
	h := sha256.New()
	h.Write(o.ClusterID[:])
	binary.Write(h, binary.BigEndian, uint64(o.OperatorID))
	binary.Write(h, binary.BigEndian, uint64(o.Height))
	binary.Write(h, binary.BigEndian, uint32(len(o.Layers)))
	for _, el := range o.Layers {
		binary.Write(h, binary.BigEndian, uint32(len(el.Value)))
		h.Write(el.Value)
		binary.Write(h, binary.BigEndian, uint32(len(el.Ciphertext)))
		h.Write(el.Ciphertext)
	}
	binary.Write(h, binary.BigEndian, uint32(len(o.NRPartials)))
	for _, p := range o.NRPartials {
		binary.Write(h, binary.BigEndian, uint32(p.Layer))
		binary.Write(h, binary.BigEndian, uint32(len(p.PartialSig)))
		h.Write(p.PartialSig)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
