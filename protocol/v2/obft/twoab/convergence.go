package twoab

// Convergence-related helpers shared between Phase 2a (LayerEntry direction
// choice) and Phase 2b (commit-trigger gate evaluation).
//
// The only Phase-2b trigger is NR-eligibility (with the cannot-σ gate),
// since KindValue is the σ-side terminal emission. The equivocation
// trigger only fires at Phase-2a fire-time for ops still in EKM
// coordination state.

// SigmaBlock discriminates an operator's σ-eligibility at a layer. It is the
// structured form of canSigmaAtLayer: canSigmaAtLayer(layer) is exactly
// (WhyNotSigma(layer) == SigmaEligible). The breakdown lets a driver (e.g.
// the consensustest classifier) tell apart failure modes that otherwise
// collapse into one ResolveFailureDeadlock walk outcome — notably a
// propagation stall (SigmaBlockNoBundle: value undelivered, recoverable on
// delivery) from a validity-divergence wedge (SigmaBlockHostInvalid: value
// received but host-invalid, unrecoverable this slot). See
// docs/2abOBFT.md §Liveness.
type SigmaBlock int

const (
	// SigmaEligible: exactly one retained leader bundle, host-validated
	// valid — the operator could emit a σ partial now.
	SigmaEligible SigmaBlock = iota
	// SigmaBlockNoBundle: no retained leader bundle (value not received).
	SigmaBlockNoBundle
	// SigmaBlockEquivocation: ≥ 2 retained bundles (leader equivocation
	// observed at this layer).
	SigmaBlockEquivocation
	// SigmaBlockHostInvalid: bundle retained, host verdict not-valid.
	SigmaBlockHostInvalid
	// SigmaBlockHostUnrecorded: bundle retained, host verdict not yet
	// consulted.
	SigmaBlockHostUnrecorded
)

// WhyNotSigma reports the operator's σ-eligibility at the given layer and,
// when blocked, why. See SigmaBlock for the discrimination and its uses.
func (i *Instance) WhyNotSigma(layer int) SigmaBlock {
	if layer < 0 || layer >= i.cfg.K() {
		return SigmaBlockNoBundle
	}
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	switch {
	case len(retained) == 0:
		return SigmaBlockNoBundle
	case len(retained) > 1:
		return SigmaBlockEquivocation
	}
	valid, recorded := i.HostValidity(layer, retained[0].Bundle.Value)
	switch {
	case !recorded:
		return SigmaBlockHostUnrecorded
	case !valid:
		return SigmaBlockHostInvalid
	}
	return SigmaEligible
}

// canSigmaAtLayer reports whether the local operator could currently emit a
// σ partial at the given layer (exactly one retained Phase-1 bundle,
// host-validated valid). Used:
//
//   - At Phase 2a fire-time: to decide between σ-chained / NR-plaintext
//     LayerEntries for L_k>0.
//   - At Phase 2b commit-trigger evaluation: as the gate condition on the
//     NR-eligibility trigger (per spec §Trigger rules / "Why NR-eligibility
//     has the cannot-σ gate"). A σ-eligible op observing novalue_pool ≥
//     qEnc before its own KindValue σ-emit must NOT emit KindCommit-NR
//     via NR-eligibility — the gate routes it to take the upgrade path
//     instead (the upgrade KindValue IS the σ-side terminal emission).
func (i *Instance) canSigmaAtLayer(layer int) bool {
	return i.WhyNotSigma(layer) == SigmaEligible
}
