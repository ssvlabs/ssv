package twoab

import (
	"bytes"
	"fmt"
	"time"
)

// BuildPhase1Bundle produces the Phase-1 bundle for `layer` on `value`.
// The local operator must be the layer's leader; otherwise returns
// ErrNotLeader.
//
// Per spec §Phase 1 (post Op3): the bundle pairs `value` with the
// leader's σ partial on V at L_0 (`L0Witness` field), signed via the
// V-keypair share. The L0Witness binds V to the leader at the protocol
// layer (closing the Variant-A withhold-then-fake-σ attack) and seeds
// `σ-pool[V_0]` with one partial from the moment the bundle is observed.
// The outer envelope's op-identity signature on the encoded bundle bytes
// is added by the SSV adapter at the wire layer.
//
// Side effects: L0Witness signing acquires the σ-side EKM lock at L_0
// (`transitionToSigma(0, value)`), so subsequent attempts to NR-direction
// at L_0 by the same leader op are blocked by EKM. Self-pools the σ
// partial into σ-pool[0][ValueRoot(value)] for the leader's own
// contribution. NOTE: under v4 the leader's σ partial was produced at
// Phase 2b (Commit-Signed); moving it to Phase 1 shifts the σ-lock
// acquisition ~1·BTT earlier in the slot (slightly enlarges the
// crash-loss window — see redesign-plan §Subtle edge cases).
//
// Idempotent: calling with the same (layer, value) repeatedly returns
// equivalent bundles (same σ partial; signer is deterministic). Calling
// with the same `layer` but different `value` would re-attempt the
// σ-lock with a contradicting value and the second call would fail via
// EKM (ErrSigmaLocked or ErrSigmaCrossV); the host-fetch loop must
// avoid this.
func (i *Instance) BuildPhase1Bundle(layer int, value Value) (*Phase1Bundle, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if layer < 0 || layer >= i.cfg.K() {
		return nil, fmt.Errorf("twoab: %w: layer %d outside [0, %d)",
			ErrLayerOutOfRange, layer, i.cfg.K())
	}
	if i.cfg.Layers[layer].Leader != i.ownOperatorID {
		return nil, fmt.Errorf("twoab: %w: local operator %d is not leader at layer %d (leader is %d)",
			ErrNotLeader, i.ownOperatorID, layer, i.cfg.Layers[layer].Leader)
	}
	if len(value) == 0 {
		return nil, ErrEmptyValue
	}

	// Sign L0Witness — only at L_0 currently (Op3 scope). For L_k>0 leader
	// witnesses see Phase D (Op8). Other layers ship with empty L0Witness
	// for now (the field exists in the wire shape uniformly; only L_0 has
	// the leader-witness mechanism in this phase).
	var l0Witness Signature
	if layer == 0 {
		// Acquire σ-side EKM lock at L_0 on this value. Failure means the
		// leader has previously locked NR (or σ on a different V) at L_0
		// — should not happen for an honest leader who fetches once per
		// slot, but the impl enforces.
		if err := i.transitionToSigma(0, value); err != nil {
			return nil, fmt.Errorf("twoab: leader L0Witness σ-lock at L_0: %w", err)
		}
		// Cache the partial in ownPartials[0] so it's available for
		// downstream reuse (later phases may consult it).
		partial, ok := i.ownPartials[0]
		if !ok {
			p, err := i.signer.SignPartial(value)
			if err != nil {
				return nil, fmt.Errorf("twoab: sign L0Witness for leader at L_0: %w", err)
			}
			i.ownPartials[0] = p
			partial = p
		}
		l0Witness = partial
		// Self-pool the leader's own σ partial into σ-pool[V_0]. The
		// leader is the first contributor; downstream receivers will
		// re-pool when they observe the bundle (idempotent under
		// addToSigmaPool's dedup-by-(layer, op) semantics).
		i.addToSigmaPool(0, ValueRoot(value), i.ownOperatorID, partial)
	}

	return &Phase1Bundle{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layer:      layer,
		Value:      append(Value{}, value...), // defensive copy
		L0Witness:  append(Signature{}, l0Witness...),
	}, nil
}

// ObservePhase1Bundle records a peer's (or the local operator's own,
// after fetch+broadcast) Phase-1 bundle. Per spec §Phase 1:
//
//   - Bundles must pass structural validation (cluster id, slot, layer
//     range, claimed leader matches the layer's designated leader,
//     non-empty value). Invalid bundles are returned with a validation
//     error; the caller's SSV adapter has already done the outer-
//     envelope auth check by this point.
//
//   - In 2abOBFT there is no auth-only / regular retention distinction
//     (no T_commit hard wall — only the runner-level slot deadline at
//     the SSV adapter caps acceptance). All in-slot bundles are retained
//     equivalently. `observedOffset` is recorded for diagnostics only.
//
//   - Up to 2 distinct value_roots are retained per (slot, layer,
//     leader_id). Per spec §Phase 1 / Retention bounds, "Further auth-
//     valid bundles for the same (slot, layer, leader_id) are dropped
//     silently." The 2-distinct cap is sufficient for Rule-2 leader-
//     equivocation evidence; the third bundle wouldn't add slashable
//     information.
//
//   - Second distinct value_root → Rule 2 (leader equivocation)
//     evidence. The pair `(BundleA, BundleB)` is self-contained
//     slashable proof (both op-identity-signed at the envelope layer;
//     receivers MAY act on a single observed pair).
//
//   - Identical re-broadcasts (same Value, observed multiple times via
//     gossipsub mesh paths) are deduplicated and silently dropped after
//     the first retention.
//
//   - On L_0 bundle observation, the Instance runs the per-tick
//     processing cascade (upgrade-check + commit-trigger-check) so a
//     KindNoValue-path op that just received V_0 can immediately emit
//     the A1 upgrade and the σ-eligibility trigger gets a chance to
//     fire on the resulting state.
func (i *Instance) ObservePhase1Bundle(b *Phase1Bundle, observedOffset time.Duration) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidatePhase1Bundle(b, i.cfg); err != nil {
		return err
	}

	if i.retainedBundles[b.Layer] == nil {
		i.retainedBundles[b.Layer] = make(map[OperatorID][]*retainedBundle)
	}
	retained := i.retainedBundles[b.Layer][b.OperatorID]

	// Dedup against already-retained value_roots.
	for _, r := range retained {
		if bytes.Equal(r.Bundle.Value, b.Value) {
			// Identical re-broadcast — silent dedup.
			return nil
		}
	}

	// Distinct value_root.
	if len(retained) >= 2 {
		// Already have 2 distinct from this leader; drop the third+.
		return nil
	}

	copyB := deepCopyBundle(b)
	newEntry := &retainedBundle{
		Bundle:                 copyB,
		RetentionEstablishedAt: observedOffset,
	}

	if len(retained) == 1 {
		// Second distinct → Rule 2 (leader equivocation).
		i.retainedBundles[b.Layer][b.OperatorID] = append(retained, newEntry)
		i.recordEvidence(Evidence{
			Rule:       EvidenceLeaderEquivocation,
			OperatorID: b.OperatorID,
			Layer:      b.Layer,
			LeaderEquivocation: &LeaderEquivocationEvidence{
				BundleA: retained[0].Bundle,
				BundleB: copyB,
			},
		})
	} else {
		// First retention.
		i.retainedBundles[b.Layer][b.OperatorID] = []*retainedBundle{newEntry}
	}

	// L0Witness verification + σ-pool seeding (Op3). Applies only at L_0
	// — deeper layers' leader witnesses are Phase D (Op8). The witness is
	// the leader's σ partial on V, verifiable against the leader's
	// pubKeyShare on V. Verification gates pool inclusion: a fake witness
	// (leader signed garbage, or byz spoofed leader bytes) fires Rule 5
	// keyed on the leader, and the witness is discarded (the bundle's V
	// is still retained for downstream pool semantics — only the
	// σ-pool[V_0] contribution is rejected).
	if b.Layer == 0 && len(b.L0Witness) > 0 {
		if i.verifyL0SigmaPartial(b.OperatorID, b.Value, b.L0Witness) {
			i.addToSigmaPool(0, ValueRoot(b.Value), b.OperatorID, b.L0Witness)
		} else if i.recordRule5(b.OperatorID, 0) {
			i.recordEvidence(Evidence{
				Rule:       EvidenceFakePlaintextSigma,
				OperatorID: b.OperatorID,
				Layer:      0,
				FakePlaintextSigma: &FakePlaintextSigmaEvidence{
					OnionPartial:        append(Signature{}, b.L0Witness...),
					OnionValue:          append(Value{}, b.Value...),
					RetainedValueHashes: i.retainedL0ValueHashes(),
				},
			})
		}
	}

	// Per-tick processing cascade: a Phase-1 bundle arrival can unlock
	// the A1 upgrade path (if this is V_0 arriving at a NoValue-path op
	// and host re-validates valid), which may in turn unlock σ-eligibility
	// or change the equivocation-trigger state. Run upgrade-first then
	// commit-trigger evaluation per §Emission ordering.
	i.afterStateDelta()

	return nil
}

// deepCopyBundle returns a defensive copy of b so retention state isn't
// affected by caller-owned slice mutation post-Observe.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	out := *b
	out.Value = append(Value{}, b.Value...)
	out.L0Witness = append(Signature{}, b.L0Witness...)
	return &out
}
