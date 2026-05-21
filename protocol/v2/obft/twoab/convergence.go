package twoab

// Convergence-related helpers shared between Phase 2a (LayerEntry direction
// choice) and Phase 2b (commit-trigger gate evaluation).
//
// Note: the bulk of the per-layer claim-pool / threshold-pool tracking and
// the three trigger predicates (σ-eligibility, NR-eligibility, equivocation)
// live elsewhere — pools incrementally maintained inside the Observe* paths
// (phase1.go, phase2a.go, phase2b.go), trigger predicates in phase2b.go.
// This file holds only the cross-phase utility helpers.

// canSigmaAtLayer reports whether the local operator could currently emit
// a σ partial at the given layer. Used:
//
//   - At Phase 2a fire-time: to decide between σ-chained / NR-plaintext
//     LayerEntries for L_k>0.
//   - At Phase 2b commit-trigger evaluation: as the gate condition on the
//     NR-eligibility trigger (per spec §Trigger rules / "Why NR-eligibility
//     has the cannot-σ gate"). A σ-eligible op observing novalue_pool ≥
//     qEnc before its own σ-eligibility view crosses qV must NOT emit
//     KindCommit-NR via NR-eligibility — the gate routes it to wait for
//     the σ-eligibility trigger instead.
//
// Returns true if all of:
//   - The layer's leader has exactly 1 retained Phase-1 bundle (no
//     equivocation observed).
//   - The retained V is host-validated as valid.
//
// Returns false otherwise (no retention, ≥ 2 retained / equivocation,
// or host says NV / host not yet consulted).
func (i *Instance) canSigmaAtLayer(layer int) bool {
	if layer < 0 || layer >= i.cfg.K() {
		return false
	}
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	if len(retained) != 1 {
		return false
	}
	valid, recorded := i.HostValidity(layer, retained[0].Bundle.Value)
	return recorded && valid
}

// vLocalAtLayer returns the operator's local V at the given layer (the
// single regularly-retained V from the layer's leader), or (nil, false)
// if the op has no V_local at this layer (0 retained, ≥ 2 retained /
// equivocation, or the retention exists but host hasn't been consulted).
//
// Used by Phase 2b's σ-eligibility-trigger side decision: when the
// trigger fires on V_k cluster-wide, each op compares their V_local to
// V_k to decide whether to emit KindCommit-Signed (matching V) or
// KindCommit-NR (non-matching V or host NV — A3 host-flip pivot).
func (i *Instance) vLocalAtLayer(layer int) (Value, bool) {
	if layer < 0 || layer >= i.cfg.K() {
		return nil, false
	}
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	if len(retained) != 1 {
		return nil, false
	}
	return append(Value{}, retained[0].Bundle.Value...), true
}

// equivocationObservedAtLayer reports whether ≥ 2 distinct V's are
// retained from the layer's leader. Used by the equivocation trigger
// at Phase 2b and by Phase 2a fire-time's NRDirect routing.
func (i *Instance) equivocationObservedAtLayer(layer int) bool {
	if layer < 0 || layer >= i.cfg.K() {
		return false
	}
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	return len(retained) >= 2
}
