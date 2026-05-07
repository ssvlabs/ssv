package obft

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

// BuildPhase1Bundle produces the Phase-1 bundle for `layer` on `value`.
// The local operator must be the layer's leader; otherwise returns an error.
//
// Per spec §Phase 1, the bundle pairs the candidate value with the leader's
// partial threshold signature on V (= one of qV partials) — giving the
// cluster a head-start on the σ-pool as soon as Phase 1 succeeds anywhere.
//
// EKM enforcement: this method also acts as the leader's σ-emit, locking
// sigmaLocked[layer] to `value`. A second BuildPhase1Bundle call at the
// same (slot, layer) with V' ≠ V is rejected (single-σ-V invariant from
// spec §Slashing-protection scope). Calling with the same V is idempotent.
func (i *Instance) BuildPhase1Bundle(layer int, value Value) (*Phase1Bundle, error) {
	if layer < 0 || layer >= i.cfg.K() {
		return nil, fmt.Errorf("obft: layer %d out of range [0, %d)", layer, i.cfg.K())
	}
	if i.cfg.Layers[layer].Leader != i.ownOperatorID {
		return nil, fmt.Errorf("obft: local operator %d is not leader at layer %d (leader is %d)",
			i.ownOperatorID, layer, i.cfg.Layers[layer].Leader)
	}
	if len(value) == 0 {
		return nil, errors.New("obft: empty value")
	}

	// EKM-style enforcement: single-σ-V per (slot, layer). The leader's
	// Phase-1 σ counts as their σ-side commitment per cross-phase
	// exclusivity (spec §Phase 2 / Per-operator commitment is exclusive).
	if err := i.transitionToSigma(layer, value); err != nil {
		return nil, fmt.Errorf("obft: σ-emit at layer %d: %w", layer, err)
	}

	// Compute and cache the σ partial. If we're idempotently re-building
	// (same V, second call), reuse the cached partial — protocol invariant
	// is "exactly one σ_V per (slot, layer)" so the second emission must
	// be byte-identical.
	partial, ok := i.ownPartials[layer]
	if !ok {
		var err error
		partial, err = i.signer.SignPartial(value)
		if err != nil {
			return nil, fmt.Errorf("obft: sign σ at layer %d: %w", layer, err)
		}
		i.ownPartials[layer] = partial
	}

	bundle := &Phase1Bundle{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layer:      layer,
		Value:      append(Value{}, value...),
		SigmaV:     partial,
	}

	// Self-observe so Resolve's σ-pool at this layer can find the leader's
	// σ_V from i.bundles. (Without this the leader's own σ_V is invisible
	// to their own Resolve walk.)
	if i.bundles[layer] == nil {
		i.bundles[layer] = make(map[OperatorID][]*Phase1Bundle)
	}
	if len(i.bundles[layer][i.ownOperatorID]) == 0 {
		i.bundles[layer][i.ownOperatorID] = []*Phase1Bundle{deepCopyBundle(bundle)}
	}

	return bundle, nil
}

// ObservePhase1Bundle records a peer's Phase-1 bundle (or the local
// operator's own bundle, after fetching). Per spec §Phase 1:
//
//   - Bundles first-observed past T_commit are rejected entirely
//     (ErrLatePhase1Bundle). The cluster relies on K-layer fall-through for
//     partition recovery (no Defer state, no late-σ-emit window).
//   - The σ-V partial is verified against the leader's pubkey share on V.
//     Bundles failing cryptographic-auth are silently dropped.
//   - Up to 2 distinct value_roots are retained per (layer, leader_id);
//     additional auth-valid bundles for the same (layer, leader_id) are
//     dropped silently. The retention bound supports leader-equivocation
//     evidence (Rule 2).
//   - Detecting a second distinct value_root from the same leader is
//     equivocation — Rule 2 evidence is recorded. Per the equivocation
//     rule, an operator who retained ≥ 2 distinct V's at this layer (and
//     has not yet σ-locked on the first) commits NR at T_commit; the
//     equivocation observation foreclose σ-emit at this layer.
func (i *Instance) ObservePhase1Bundle(b *Phase1Bundle, observedOffset time.Duration) error {
	if err := ValidatePhase1Bundle(b, i.cfg); err != nil {
		return err
	}
	if !i.observedTimeOK(observedOffset) {
		return ErrLatePhase1Bundle
	}

	// Cryptographic-auth check on σ_V against the leader's pubkey share.
	leaderShare, ok := i.pubKeyShares[b.OperatorID]
	if !ok {
		// Leader's share not registered — treat as auth failure.
		return fmt.Errorf("obft: no pubkey share for leader %d", b.OperatorID)
	}
	if !i.signer.VerifyPartial(leaderShare, b.Value, b.SigmaV) {
		return fmt.Errorf("obft: phase-1 bundle σ_V does not verify against leader %d's share",
			b.OperatorID)
	}

	// Look up the per-leader retention slot for this layer.
	if i.bundles[b.Layer] == nil {
		i.bundles[b.Layer] = make(map[OperatorID][]*Phase1Bundle)
	}
	retained := i.bundles[b.Layer][b.OperatorID]

	// Dedup against retained bundles.
	for _, r := range retained {
		if bytes.Equal(r.Value, b.Value) {
			// Same value_root already retained — drop silently. (An honest
			// leader broadcasts at most one bundle, but gossipsub may
			// re-deliver; the existing entry is canonical.)
			return nil
		}
	}

	// Distinct value_root. If we already have one retained, this is
	// equivocation evidence (Rule 2).
	if len(retained) >= 1 {
		// Cap retention at 2 distinct.
		if len(retained) >= 2 {
			// Already have 2 distinct from this leader; drop the third
			// (and beyond). The first two are sufficient evidence.
			return nil
		}
		// Retain the second distinct; record Rule 2 evidence.
		copyB := deepCopyBundle(b)
		i.bundles[b.Layer][b.OperatorID] = append(retained, copyB)
		i.recordEvidence(Evidence{
			Rule:       EvidenceLeaderEquivocation,
			OperatorID: b.OperatorID,
			Layer:      b.Layer,
			LeaderEquivocation: &LeaderEquivocationEvidence{
				BundleA: retained[0],
				BundleB: copyB,
			},
		})
		// Local-state effect of equivocation per spec §Phase 1 / Equivocation
		// handling: at T_commit, an operator with ≥ 2 distinct V's retained
		// emits NR (per the equivocation rule, no winner-picking under f=1
		// byzantine). Pre-T_commit, leave the state alone — chosenVForLayer
		// will return false (≥ 2 retained = no unique V) so BuildOwnCommit
		// will commit NR for this layer at T_commit.
		//
		// If the operator already σ-locked on the first V (the byzantine
		// delivered it before observing equivocation), they stay σ-locked
		// per cross-phase exclusivity.
		return nil
	}

	// First retention for this (layer, leader). Stash a defensive deep copy
	// so retained bundle bytes are independent of caller-owned slices.
	i.bundles[b.Layer][b.OperatorID] = []*Phase1Bundle{deepCopyBundle(b)}
	return nil
}

// deepCopyBundle returns a deep copy of b — the byte slices inside (Value,
// SigmaV) are independent of the source. Used at retention boundaries so
// caller-owned slices can be modified without corrupting Instance state.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	return &Phase1Bundle{
		ClusterID:  b.ClusterID,
		OperatorID: b.OperatorID,
		Height:     b.Height,
		Layer:      b.Layer,
		Value:      append(Value{}, b.Value...),
		SigmaV:     append(Signature{}, b.SigmaV...),
	}
}

// ApplyHostValidity records the host application's valid/not-valid verdict
// for `value` at `layer`. Per spec, the host's check should be run once at
// Phase-1 acceptance against a stable head snapshot, then locked for the
// remainder of the slot — but the protocol itself just consumes the verdict
// and does not interpret the host's reasoning.
//
// The verdict is recorded per (layer, V) and consulted by BuildOwnCommit at
// T_commit: if the operator's uniquely retained V at this layer is recorded
// as not-valid, they emit NV (operationally identical to NR on the wire);
// otherwise they σ-emit on V.
//
// The verdict is recorded per (layer, V) since multiple V's may exist at a
// layer under leader equivocation (though equivocation collapses to NR at
// T_commit, regardless of validity verdicts on either V).
func (i *Instance) ApplyHostValidity(layer int, value Value, valid bool) error {
	if layer < 0 || layer >= i.cfg.K() {
		return fmt.Errorf("obft: layer %d out of range", layer)
	}
	if len(value) == 0 {
		return errors.New("obft: empty value")
	}
	if i.hostVerdict[layer] == nil {
		i.hostVerdict[layer] = make(map[string]bool)
	}
	key := valueRootKey(value)
	// Per-operator validity-locking discipline: do not flip a verdict once
	// recorded.
	if existing, recorded := i.hostVerdict[layer][key]; recorded {
		if existing != valid {
			return fmt.Errorf("obft: host validity verdict for layer %d already locked (was %v, now %v)",
				layer, existing, valid)
		}
		return nil
	}
	i.hostVerdict[layer][key] = valid
	return nil
}

// chosenVForLayer returns the operator's σ-target V at `layer` if uniquely
// determined: there is exactly one retained Phase-1 bundle (across all
// leaders, but in practice one leader per layer) AND the host validated it.
// Returns (nil, false) if equivocation, no retained V, or host hasn't
// validated.
func (i *Instance) chosenVForLayer(layer int) (Value, bool) {
	leaderMap := i.bundles[layer]
	if len(leaderMap) == 0 {
		return nil, false
	}
	// In OBFT, only the layer's designated leader is allowed to broadcast
	// Phase-1 bundles. ValidatePhase1Bundle enforces this. So at most one
	// leader entry exists; check it.
	expectedLeader := i.cfg.Layers[layer].Leader
	retained := leaderMap[expectedLeader]
	if len(retained) != 1 {
		return nil, false
	}
	v := retained[0].Value
	verdicts := i.hostVerdict[layer]
	if verdicts == nil {
		return nil, false
	}
	valid, recorded := verdicts[valueRootKey(v)]
	if !recorded || !valid {
		return nil, false
	}
	return v, true
}
