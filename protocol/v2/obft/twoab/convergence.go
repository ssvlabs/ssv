package twoab

// Convergence-related helpers shared between Phase 2a (LayerEntry direction
// choice) and Phase 2b (commit-trigger gate evaluation).
//
// Post Op5, the only remaining Phase-2b trigger is NR-eligibility (with the
// cannot-σ gate). The σ-eligibility trigger was removed when KindValue
// became the σ-side terminal emission; the equivocation trigger only fires
// at Phase-2a fire-time for ops still in EKM coordination state.

// canSigmaAtLayer reports whether the local operator could currently emit
// a σ partial at the given layer. Used:
//
//   - At Phase 2a fire-time: to decide between σ-chained / NR-plaintext
//     LayerEntries for L_k>0.
//   - At Phase 2b commit-trigger evaluation: as the gate condition on the
//     NR-eligibility trigger (per spec §Trigger rules / "Why NR-eligibility
//     has the cannot-σ gate"). A σ-eligible op observing novalue_pool ≥
//     qEnc before its own KindValue σ-emit must NOT emit KindCommit-NR
//     via NR-eligibility — the gate routes it to take the A1 upgrade
//     path instead (under Op5 the A1 upgrade KindValue IS the σ-side
//     terminal emission).
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
