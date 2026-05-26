package consensustest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// OfflineAggregator records every threshold-shared partial signature that hits
// the wire during a sim, irrespective of which honest operator received it.
// After the sim ends, AttemptAll attempts every reconstruction a worst-case
// byzantine with full message visibility could try: direct σ-quorums at L_0,
// chained-decrypt σ-quorums at L_k>0 unlocked by observed NR-quorums.
//
// The Pigeonhole 2 + Pigeonhole 3 safety claim is that no offline aggregator
// can reconstruct two distinct V signatures. A SafetyReport with
// NoOfflineDoubleV=false is a load-bearing failure (not just a non-decision)
// and panics in the runner.
//
// Reconstruction is cardinality+hash based in both stub-BLS and real-BLS
// modes — the aggregator counts distinct partials per (layer, value-hash)
// bucket and reports reconstruction-feasible if any bucket meets QV.
// It does NOT run actual BLS threshold aggregation; the safety claim it
// validates is "no offline aggregator could COLLECT ≥ qV partials on two
// distinct V's", which is independent of whether the partials would
// cryptographically aggregate (the protocol's per-partial verify, run
// inside obft.Instance, already gates that). Adding real-BLS aggregation
// here would catch the same class of safety violation; the value would
// be belt-and-braces confirmation that QV partials genuinely aggregate
// into a valid full signature — a property the production OBFT path
// already exercises.
type OfflineAggregator struct {
	// SigmaPartials groups observed σ partials by (layer, value-hash).
	// Each bucket records the contributing operators; a quorum (≥ QV) on
	// distinct contributors makes that V reconstructable at L_0 directly,
	// or at L_k>0 after chain unlock.
	SigmaPartials map[SigmaKey]map[OperatorID]struct{}

	// EncryptedClaims groups encrypted-onion entries by (layer, value-hash).
	// Each emitter-op gets one claim per (layer, V). After chain unlock, a
	// quorum of distinct claims on the same V reconstructs σ at that layer.
	EncryptedClaims map[SigmaKey]map[OperatorID]struct{}

	// NRPartials groups observed NR partials by layer. NR-tag is per-layer
	// (= H(layer) bound to cluster context); a quorum of NR partials at
	// layer k unlocks the chain for k+1.
	NRPartials map[int]map[OperatorID]struct{}

	// SigmaByEmitter records per-(actual-emitter, layer, value-hash) σ-side
	// commitments — covers both plaintext σ partials and encrypted-claim σ
	// entries. Distinct from SigmaPartials/EncryptedClaims (which key by
	// c.OperatorID, the claimed sender) because byzantine identity forgery
	// would mislead per-op invariant checks. The actual emitter is the
	// operator who built and emitted the message, regardless of what
	// c.OperatorID was stamped. Adapters call ObserveSigmaByEmitter at
	// observation sites passing the genuine sender.
	//
	// Consumed by bucket-2 per-op invariants:
	//   - B1 cross-phase exclusivity (σ-XOR-NR per layer): no honest emitter
	//     appears in both SigmaByEmitter and NRByEmitter at the same layer.
	//   - B2 single-σ-V per layer: no honest emitter appears in
	//     SigmaByEmitter at the same layer with two distinct value_hashes.
	//
	// Honest-vs-byz filtering is applied at check time using Outcome.Byz,
	// not at observation time — the maps stay complete for diagnostic dumps.
	SigmaByEmitter map[ByEmitterSigmaKey]struct{}

	// NRByEmitter records per-(actual-emitter, layer) NR-tag partial
	// commitments. Parallel to SigmaByEmitter; bucket-2 B1 reads both.
	NRByEmitter map[ByEmitterNRKey]struct{}

	// QV is the σ-quorum threshold (= 2f+1 from cfg.N).
	QV int

	// QEnc is the NR-quorum threshold (= 2f+1; same as QV in OBFT).
	QEnc int
}

// ByEmitterSigmaKey identifies a (actual-emitter, layer, value-hash) σ
// observation. Distinct from SigmaKey (which omits emitter) — see
// OfflineAggregator.SigmaByEmitter docstring.
type ByEmitterSigmaKey struct {
	Emitter   OperatorID
	Layer     int
	ValueHash [32]byte
}

// ByEmitterNRKey identifies a (actual-emitter, layer) NR observation.
type ByEmitterNRKey struct {
	Emitter OperatorID
	Layer   int
}

// SigmaKey identifies a (layer, value-hash) bucket.
type SigmaKey struct {
	Layer     int
	ValueHash [32]byte
}

// OfflineReconstruction describes one V signature an offline aggregator could
// build from observed partials. Path is a human-readable trace of how the
// reconstruction succeeded.
type OfflineReconstruction struct {
	Layer     int
	ValueHash [32]byte
	Path      string // e.g. "L_0 σ-quorum from {1,2,3}"
}

// OfflineAggReport is the post-sim verdict from the offline aggregator,
// stored in Outcome.OfflineAgg. NoOfflineDoubleV=false is a safety violation.
type OfflineAggReport struct {
	NoOfflineDoubleV bool
	Reconstructions  []OfflineReconstruction

	// SigmaCardinality is the cluster-wide σ-pool cardinality per (layer,
	// value_hash) bucket — the count an offline aggregator could collect
	// toward σ-quorum. At L_0: equals |SigmaPartials[(0, V)]| (plaintext
	// σ partials). At L_k>0: equals |SigmaPartials[(k, V)]| (plaintext
	// leader-σ_V + witness-section partials) PLUS |EncryptedClaims[(k, V)]|
	// when the chain is unlocked (NR-quorum reached at every shallower
	// layer); equals just |SigmaPartials[(k, V)]| when chain is sealed.
	// Pre-computed in AttemptAll. Plumbing for diagnostic + a future C1
	// QuorumBackedDecision check (which needs protocol-side per-decision
	// quorum-count emission to disambiguate underapproximation vs
	// regression — see docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md).
	// Not consumed by any safety check today.
	SigmaCardinality map[SigmaKey]int

	// SigmaByEmitter / NRByEmitter mirror the same-named OfflineAggregator
	// maps — exposed on the post-sim report so consumers reading
	// Outcome.OfflineAgg (e.g., ComputeSafetyReport's bucket-2 honest-op
	// invariants) can iterate them without holding a reference to the live
	// OfflineAggregator. See OfflineAggregator's SigmaByEmitter docstring
	// for the claimed-sender-vs-actual-emitter semantics.
	//
	// WARNING — shared map references with the live aggregator: AttemptAll
	// shares (no copy) for cost-savings. Safe under the contract "the
	// aggregator is discarded by the adapter post-AttemptAll" — every
	// adapter today follows this. If a future caller re-uses the
	// aggregator (e.g., calls AttemptAll twice, holds the aggregator for
	// diagnostics while also retaining the report, or calls Observe*
	// methods post-AttemptAll), these maps mutate under the report's
	// feet, which would break any safety check or diagnostic that
	// captured the report and expected stable data. If you need that
	// usage pattern, deep-copy here in AttemptAll instead of aliasing.
	SigmaByEmitter map[ByEmitterSigmaKey]struct{}
	NRByEmitter    map[ByEmitterNRKey]struct{}
}

// NewOfflineAggregator returns an empty aggregator sized for cluster N.
func NewOfflineAggregator(n int) *OfflineAggregator {
	f := (n - 1) / 3
	q := 2*f + 1
	return &OfflineAggregator{
		SigmaPartials:   make(map[SigmaKey]map[OperatorID]struct{}),
		EncryptedClaims: make(map[SigmaKey]map[OperatorID]struct{}),
		NRPartials:      make(map[int]map[OperatorID]struct{}),
		SigmaByEmitter:  make(map[ByEmitterSigmaKey]struct{}),
		NRByEmitter:     make(map[ByEmitterNRKey]struct{}),
		QV:              q,
		QEnc:            q,
	}
}

// hashValue returns the SHA-256 hash used as a stable map key for V bytes.
func hashValue(v []byte) [32]byte {
	return sha256.Sum256(v)
}

// ObserveSigma records that op contributed a plaintext σ partial at layer for
// value V. (For L_0 σ partials on the wire, or for any σ partial that
// becomes accessible after chain-decrypt.)
func (a *OfflineAggregator) ObserveSigma(op OperatorID, layer int, v []byte) {
	a.observeSigmaHash(op, layer, hashValue(v))
}

// ObserveSigmaByValueRoot is like ObserveSigma but takes a pre-computed
// 32-byte value_root (sha256(V)) instead of V — used when the caller has
// the value_root identifier without the full V (e.g., processing a
// LeaderSigmaWitness from a Commit).
func (a *OfflineAggregator) ObserveSigmaByValueRoot(op OperatorID, layer int, valueRoot [32]byte) {
	a.observeSigmaHash(op, layer, valueRoot)
}

func (a *OfflineAggregator) observeSigmaHash(op OperatorID, layer int, hash [32]byte) {
	k := SigmaKey{Layer: layer, ValueHash: hash}
	if a.SigmaPartials[k] == nil {
		a.SigmaPartials[k] = make(map[OperatorID]struct{})
	}
	a.SigmaPartials[k][op] = struct{}{}
}

// ObserveEncryptedClaim records that op's Commit at layer carried an
// encrypted onion entry claiming to wrap V. Multiple ops may claim the same
// (layer, V); chain-unlock + ≥ QV distinct claimers reconstructs σ at that
// layer.
func (a *OfflineAggregator) ObserveEncryptedClaim(op OperatorID, layer int, v []byte) {
	k := SigmaKey{Layer: layer, ValueHash: hashValue(v)}
	if a.EncryptedClaims[k] == nil {
		a.EncryptedClaims[k] = make(map[OperatorID]struct{})
	}
	a.EncryptedClaims[k][op] = struct{}{}
}

// ObserveSigmaByEmitter records a σ-side commitment from the named actual
// emitter on V at layer. Plaintext σ partials, encrypted-claim σ entries,
// and any other σ-side EKM commitment paths all flow through this call;
// the by-emitter view doesn't distinguish wire format because B2 (single-σ-V
// per layer) is a property of the operator's signing decision, not the
// wire-format the partial rides in.
//
// Callers MUST pass the actual emitter (the operator who built and emitted
// the message), not the claimed sender c.OperatorID — under byzantine
// identity forgery the two differ, and per-op invariants apply to the
// emitter's own commitment.
//
// Distinct from ObserveSigma, which keys on claimed-sender for the
// aggregator-bypass detection model.
func (a *OfflineAggregator) ObserveSigmaByEmitter(emitter OperatorID, layer int, v []byte) {
	a.SigmaByEmitter[ByEmitterSigmaKey{
		Emitter:   emitter,
		Layer:     layer,
		ValueHash: hashValue(v),
	}] = struct{}{}
}

// ObserveNRByEmitter records an NR-side commitment from the named actual
// emitter at layer. Same emitter-vs-claimed-sender distinction as
// ObserveSigmaByEmitter.
func (a *OfflineAggregator) ObserveNRByEmitter(emitter OperatorID, layer int) {
	a.NRByEmitter[ByEmitterNRKey{
		Emitter: emitter,
		Layer:   layer,
	}] = struct{}{}
}

// ObserveNR records that op contributed an NR partial at layer.
func (a *OfflineAggregator) ObserveNR(op OperatorID, layer int) {
	if a.NRPartials[layer] == nil {
		a.NRPartials[layer] = make(map[OperatorID]struct{})
	}
	a.NRPartials[layer][op] = struct{}{}
}

// AttemptAll returns every distinct V signature an offline aggregator could
// reconstruct. NoOfflineDoubleV is true iff there is at most one distinct V.
//
// Reconstruction strategies:
//   - Direct σ-quorum: any (layer, V) with ≥ QV σ partials from distinct ops
//     yields a reconstruction at that layer.
//   - Chained-decrypt σ-quorum: any (layer, V) at layer > 0 with ≥ QV
//     EncryptedClaims AND NR-quorum on every shallower layer (chain unlock)
//     yields a reconstruction.
//
// Reconstructions are deduplicated by ValueHash cluster-wide: the same V is
// recorded at most once, at the shallowest layer it was reconstructable
// from. This is correct for the NoOfflineDoubleV count (which asks "how many
// distinct V signatures?") but means the slice doesn't enumerate every
// (layer, V) pair an aggregator could compute — a V reconstructable at both
// L_0 (σ-quorum) and L_1 (chained-decrypt) appears only at L_0.
//
// Chain-unlock is approximated as "every shallow layer has SOME NR-quorum"
// rather than per-V chain matching; this is permissive (over-counts
// reconstructions) so false positives risk only spurious safety panics, not
// missed violations.
func (a *OfflineAggregator) AttemptAll() OfflineAggReport {
	rep := OfflineAggReport{
		NoOfflineDoubleV: true,
		SigmaCardinality: make(map[SigmaKey]int),
		// Share map references — the aggregator is discarded post-AttemptAll
		// by adapter callers, so no aliasing hazard.
		SigmaByEmitter: a.SigmaByEmitter,
		NRByEmitter:    a.NRByEmitter,
	}
	seen := make(map[[32]byte]struct{})

	// Pre-compute SigmaCardinality across every observed bucket. At L_0:
	// plaintext SigmaPartials only. At L_k>0: SigmaPartials (plaintext
	// leader σ_V + witness-section partials) + EncryptedClaims when the
	// chain is unlocked. The cardinality is the union-cardinality across
	// emitters (dedup per emitter); we approximate it as the sum of the
	// two distinct-emitter sets since SigmaPartials and EncryptedClaims at
	// the same (layer, V) are populated from different message-sections so
	// rarely double-count the same emitter — and any overcount is benign
	// (lets the count reach qV legitimately).
	for k, partials := range a.SigmaPartials {
		rep.SigmaCardinality[k] = len(partials)
	}
	for k, claims := range a.EncryptedClaims {
		if k.Layer == 0 {
			// Defensive: under current adapters (obft/events.go +
			// twoab/events.go) L_0 plaintext is routed through
			// ObserveSigma → SigmaPartials, never through
			// ObserveEncryptedClaim → EncryptedClaims. This branch is
			// unreachable today; kept against a future spec change
			// where a LayerEntry[0] with SigmaChained kind would
			// otherwise double-count into SigmaPartials.
			continue
		}
		unlocked := true
		for shallow := 0; shallow < k.Layer; shallow++ {
			if len(a.NRPartials[shallow]) < a.QEnc {
				unlocked = false
				break
			}
		}
		if !unlocked {
			continue
		}
		rep.SigmaCardinality[k] += len(claims)
	}

	// Direct σ-quorums (primarily L_0 in OBFT, but applicable at any layer
	// where σ is recorded plaintext on the wire).
	keys := sortedSigmaKeys(a.SigmaPartials)
	for _, k := range keys {
		if len(a.SigmaPartials[k]) < a.QV {
			continue
		}
		if _, dup := seen[k.ValueHash]; dup {
			continue
		}
		seen[k.ValueHash] = struct{}{}
		ops := sortedOps(a.SigmaPartials[k])
		rep.Reconstructions = append(rep.Reconstructions, OfflineReconstruction{
			Layer:     k.Layer,
			ValueHash: k.ValueHash,
			Path:      fmt.Sprintf("L_%d σ-quorum from %v", k.Layer, ops),
		})
	}

	// Chained-decrypt σ-quorums at deeper layers.
	encKeys := sortedSigmaKeys(a.EncryptedClaims)
	for _, k := range encKeys {
		if k.Layer == 0 {
			continue // L_0 is plaintext, handled by SigmaPartials
		}
		if len(a.EncryptedClaims[k]) < a.QV {
			continue
		}
		if _, dup := seen[k.ValueHash]; dup {
			continue
		}
		unlocked := true
		for shallow := 0; shallow < k.Layer; shallow++ {
			if len(a.NRPartials[shallow]) < a.QEnc {
				unlocked = false
				break
			}
		}
		if !unlocked {
			continue
		}
		seen[k.ValueHash] = struct{}{}
		ops := sortedOps(a.EncryptedClaims[k])
		rep.Reconstructions = append(rep.Reconstructions, OfflineReconstruction{
			Layer:     k.Layer,
			ValueHash: k.ValueHash,
			Path:      fmt.Sprintf("L_%d chained-decrypt σ-quorum from %v", k.Layer, ops),
		})
	}

	if len(rep.Reconstructions) > 1 {
		rep.NoOfflineDoubleV = false
	}
	return rep
}

func sortedSigmaKeys(m map[SigmaKey]map[OperatorID]struct{}) []SigmaKey {
	keys := make([]SigmaKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Layer != keys[j].Layer {
			return keys[i].Layer < keys[j].Layer
		}
		return bytes.Compare(keys[i].ValueHash[:], keys[j].ValueHash[:]) < 0
	})
	return keys
}

func sortedOps(m map[OperatorID]struct{}) []OperatorID {
	ops := make([]OperatorID, 0, len(m))
	for op := range m {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i] < ops[j] })
	return ops
}

// String renders Reconstructions for diagnostic dumps.
func (r OfflineAggReport) String() string {
	if r.NoOfflineDoubleV {
		if len(r.Reconstructions) == 0 {
			return "OfflineAgg: no reconstructions"
		}
		return fmt.Sprintf("OfflineAgg: 1 reconstruction (%s)", r.Reconstructions[0].Path)
	}
	parts := make([]string, len(r.Reconstructions))
	for i, rec := range r.Reconstructions {
		parts[i] = fmt.Sprintf("L_%d V=%s via %s",
			rec.Layer, hex.EncodeToString(rec.ValueHash[:6]), rec.Path)
	}
	out := "OfflineAgg: DOUBLE-V VIOLATION"
	for _, p := range parts {
		out += "\n  " + p
	}
	return out
}
