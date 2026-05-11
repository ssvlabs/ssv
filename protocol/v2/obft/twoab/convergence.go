package twoab

import "fmt"

// CommitChoice represents the per-layer Phase-2b convergence-rule
// decision: σ on a specific V, or NR.
type CommitChoice int

const (
	// CommitChoiceSigma — the operator commits σ on the layer's V.
	// `value` from ConvergenceDecision is non-nil; the runner builds a
	// σ partial on that V (plaintext at L_0; chained-IBE-encrypted at
	// deeper layers).
	CommitChoiceSigma CommitChoice = iota

	// CommitChoiceNR — the operator commits NR at this layer. For
	// layers in [0, K-1) this produces an `σ_i^{IBE}(nr_tag_k)` partial
	// on the wire. At layer K-1 (deepest) there is no NR tag, so NR
	// produces no on-wire emission per spec §Convergence rule
	// "Deepest-layer NR has no on-wire emission".
	CommitChoiceNR
)

// String returns a label for telemetry/logging.
func (c CommitChoice) String() string {
	switch c {
	case CommitChoiceSigma:
		return "sigma"
	case CommitChoiceNR:
		return "NR"
	default:
		return "unknown"
	}
}

// ConvergenceDecision applies the 5-row decision table from spec §Phase 2b
// "Convergence rule" at the given layer. Returns the commit choice and,
// for σ-side, the V to sign over.
//
// The decision is computed at Phase-2b sign time using:
//   - The operator's local retention state at this layer (V_local).
//   - The cluster-wide Phase-2a verdict pool, with equivocators excluded
//     per the treat-as-null SHOULD rule (Phase F's verdictEquivocator).
//   - The host's re-validation of V_local at Phase-2b sign time (host
//     may have flipped from valid to not-valid since Phase 2a — narrows
//     the validity-divergence window).
//
// Rows evaluated in order — first match wins:
//
//  1. Equivocation observed (≥ 2 distinct V's retained at this layer
//     by this operator) → NR (NR-due-to-equivocation; V_local undefined).
//  2. nr_eligibility_quorum reached (|nr_pool| ≥ qEnc) → NR (honest
//     defers to the cluster's NR-side decision regardless of own
//     verdict).
//  3. σ_eligibility_quorum reached on V (∃V: |verdict_pool[V]| ≥ qV)
//     AND operator has V_local = V AND host re-validates V as valid at
//     Phase-2b sign time → σ on V.
//  4. σ_eligibility_quorum reached on V AND operator doesn't have
//     V_local = V (or host re-validation says NV) → NR.
//  5. Neither quorum reached → NR (verdict-quorum short; cluster
//     didn't converge on V; honest defaults to NR).
//
// At n = 3f+1 (the SSV BFT bound) rows 2 and 3 cannot both fire — see
// spec §Phase 2b "NR-vs-σ tightness at n = 3f+1". At higher n,
// nr_eligibility_quorum overrides per row 2's ordering.
//
// Returns ErrLayerOutOfRange if `layer` is outside [0, K). Otherwise
// always returns (choice, value, nil) — host-validity-missing for the
// retained V is treated as "host hasn't re-validated yet" and routes
// through row 4 → NR; no error.
func (i *Instance) ConvergenceDecision(layer int) (CommitChoice, Value, error) {
	if layer < 0 || layer >= i.cfg.K() {
		return CommitChoiceNR, nil, fmt.Errorf("twoab: %w: layer %d outside [0, %d)",
			ErrLayerOutOfRange, layer, i.cfg.K())
	}

	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]

	// Row 1: Equivocation observed (≥ 2 distinct V's retained at this
	// layer; counts both regular and auth-only retention).
	if len(retained) >= 2 {
		return CommitChoiceNR, nil, nil
	}

	// V_local: the operator's single regular-retention V at this layer
	// (or none, if 0 retained or only auth-only retention exists).
	var vLocal Value
	hasVLocal := false
	if len(retained) == 1 && !retained[0].AuthOnly {
		vLocal = retained[0].Bundle.Value
		hasVLocal = true
	}

	qV := i.cfg.QV()
	qEnc := i.cfg.QEnc()

	// Build the convergence pools with treat-as-null applied.
	// peerVerdicts holds the FIRST-observed verdict per peer; ownVerdict
	// holds the local op's broadcast verdict (Phase F).
	verdictPool, nrPool := i.buildConvergencePools(layer)

	// Row 2: nr_eligibility_quorum.
	if len(nrPool) >= qEnc {
		return CommitChoiceNR, nil, nil
	}

	// Row 3/4: σ_eligibility_quorum?
	for vRoot, ops := range verdictPool {
		if len(ops) < qV {
			continue
		}
		// σ-eligibility met on this V_root.
		// At n = 3f+1, at most one V can have qV (Pigeonhole bound per
		// spec §Phase 2b "Tie-break note"); we still iterate cleanly.
		if !hasVLocal {
			return CommitChoiceNR, nil, nil // Row 4
		}
		localRoot := ValueRoot(vLocal)
		if localRoot != vRoot {
			return CommitChoiceNR, nil, nil // Row 4 — different V
		}
		// Local V matches the σ-eligible V — re-validate against host.
		valid, recorded := i.HostValidity(layer, vLocal)
		if !recorded || !valid {
			return CommitChoiceNR, nil, nil // Row 4 — host says NV at sign time
		}
		return CommitChoiceSigma, append(Value{}, vLocal...), nil // Row 3
	}

	// Row 5: Neither quorum reached.
	return CommitChoiceNR, nil, nil
}

// buildConvergencePools returns (verdict_pool[V] → ops, nr_pool → ops)
// for the given layer, including the local op's own verdict and
// excluding any operator flagged as a verdict equivocator (treat-as-null
// SHOULD rule from spec §Phase 2a).
//
// Note: σV verdicts contribute to verdict_pool[V] keyed by ValueRoot;
// NR/NV verdicts contribute to nr_pool. VerdictUnspecified (or any
// unknown kind) is treated as no contribution (defensive against
// malformed peer state).
func (i *Instance) buildConvergencePools(layer int) (map[[32]byte][]OperatorID, []OperatorID) {
	verdictPool := make(map[[32]byte][]OperatorID)
	var nrPool []OperatorID

	// Helper to add a verdict to the appropriate pool. Equivocators are
	// excluded via treat-as-null at the call site.
	addToPool := func(op OperatorID, v *Verdict) {
		if v == nil {
			return
		}
		switch v.Kind {
		case VerdictSigmaV:
			verdictPool[v.ValueRoot] = append(verdictPool[v.ValueRoot], op)
		case VerdictNR, VerdictNV:
			nrPool = append(nrPool, op)
		}
	}

	// Include the local op's own verdict.
	if own, ok := i.ownVerdict[layer]; ok {
		// If the local op's own verdict came back as σV, the equivocator
		// flag wouldn't apply (we can't equivocate against ourselves —
		// BuildVerdict is idempotent). Skip the IsEquivocator check.
		addToPool(i.ownOperatorID, own)
	}

	// Include peer verdicts, excluding equivocators.
	for op, v := range i.peerVerdicts[layer] {
		if i.IsEquivocator(layer, op) {
			continue
		}
		addToPool(op, v)
	}

	return verdictPool, nrPool
}

// chosenVAtLayer returns the operator's single regularly-retained V at
// the layer, or (nil, false) if the operator has no V_local (0 retained
// or only auth-only retention or equivocation observed).
//
// Helper for tests; the convergence rule duplicates this logic inline
// for efficiency.
func (i *Instance) chosenVAtLayer(layer int) (Value, bool) {
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	if len(retained) != 1 {
		return nil, false
	}
	if retained[0].AuthOnly {
		return nil, false
	}
	return append(Value{}, retained[0].Bundle.Value...), true
}
