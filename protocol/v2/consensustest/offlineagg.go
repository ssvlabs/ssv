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
// In stub-BLS mode, "reconstruction" is shallow — the framework verifies
// quorum-cardinality and pubkey-distinctness but does not run real BLS
// math. In real-BLS mode (cfg.BLSKeys set), reconstruction performs actual
// threshold aggregation + verification. Both modes catch the same class of
// safety violation; real-BLS additionally validates the cryptographic
// primitive's threshold property.
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

	// QV is the σ-quorum threshold (= 2f+1 from cfg.N).
	QV int

	// QEnc is the NR-quorum threshold (= 2f+1; same as QV in OBFT).
	QEnc int
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
}

// NewOfflineAggregator returns an empty aggregator sized for cluster N.
func NewOfflineAggregator(n int) *OfflineAggregator {
	f := (n - 1) / 3
	q := 2*f + 1
	return &OfflineAggregator{
		SigmaPartials:   make(map[SigmaKey]map[OperatorID]struct{}),
		EncryptedClaims: make(map[SigmaKey]map[OperatorID]struct{}),
		NRPartials:      make(map[int]map[OperatorID]struct{}),
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
	k := SigmaKey{Layer: layer, ValueHash: hashValue(v)}
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
// Chain-unlock is approximated as "every shallow layer has SOME NR-quorum"
// rather than per-V chain matching; this is permissive (over-counts
// reconstructions) so false positives risk only spurious safety panics, not
// missed violations.
func (a *OfflineAggregator) AttemptAll() OfflineAggReport {
	rep := OfflineAggReport{NoOfflineDoubleV: true}
	seen := make(map[[32]byte]struct{})

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
