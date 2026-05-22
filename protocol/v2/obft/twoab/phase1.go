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
	//
	// Sequencing: sign first, THEN acquire σ-lock + self-pool. A signer
	// failure (BLS infra issue) must NOT leave the EKM σ-locked without
	// a corresponding partial existing — that state would block any
	// further L_0 emission from this leader without a recoverable retry
	// path. By signing first (no state mutation on failure), we ensure
	// the leader can retry the build later if the signer transiently
	// fails.
	var l0Witness Signature
	if layer == 0 {
		// 1) Compute the partial. SignPartial is pure (no Instance state
		// mutation); if it errors, we abort with no side effects.
		partial, ok := i.ownPartials[0]
		if !ok {
			p, err := i.signer.SignPartial(value)
			if err != nil {
				return nil, fmt.Errorf("twoab: sign L0Witness for leader at L_0: %w", err)
			}
			partial = p
		}
		// 2) Acquire σ-side EKM lock at L_0 on this value. Failure means
		// the leader has previously locked NR or σ on a different V at
		// L_0 — should not happen for an honest leader who fetches once
		// per slot, but the impl enforces. We abort BEFORE caching the
		// partial / pooling, so a lock-fail leaves Instance state
		// untouched (the signed bytes are discarded).
		if err := i.transitionToSigma(0, value); err != nil {
			return nil, fmt.Errorf("twoab: leader L0Witness σ-lock at L_0: %w", err)
		}
		// 3) Both sign and lock succeeded — commit the partial to cache
		// and self-pool into σ-pool[V_0]. Idempotent on subsequent calls
		// with the same value (cache hit on ownPartials, idempotent
		// addToSigmaPool semantics).
		i.ownPartials[0] = partial
		l0Witness = partial
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
//     slashable proof. Cryptographic basis depends on the retention
//     entry point:
//     (a) Both retained via direct ObservePhase1Bundle: the bundles'
//     outer envelopes are op-identity-signed by the leader, so
//     the envelope signatures themselves prove leader emission
//     of two distinct V's.
//     (b) One or both retained via the Op11 harvest path (synthetic
//     bundle reconstructed from a peer KindValue): the outer
//     envelope is the *emitter's*, not the leader's — but the
//     L0Witness on each retained bundle is the leader's BLS σ
//     partial on V, pre-verified by the receiver against the
//     leader's pubKeyShare before retention. Two valid leader-
//     signed partials on distinct V_a / V_b is itself
//     cryptographic proof of leader equivocation (BLS-share
//     unforgeability under our threshold assumption).
//     Either way, receivers MAY act on a single observed pair.
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
	// Direct-observation path: the bundle's outer envelope is op-identity-
	// signed by the claimed leader (the SSV adapter has verified this before
	// reaching the protocol layer). That cryptographic binding is what makes
	// Rule 5 attribution to the leader sound on L0Witness verify-fail —
	// hence witnessPreVerified=false (let retainPhase1Bundle verify and
	// fire Rule 5 if needed).
	i.retainPhase1Bundle(b, observedOffset, false /* witnessPreVerified */)
	return nil
}

// retainPhase1Bundle is the shared retention path for Phase-1 bundles,
// invoked by both ObservePhase1Bundle (direct observation) and the Op11
// peer-harvest path in ObserveValueMsg (synthetic bundles reconstructed
// from a verified peer-KindValue L0Witness).
//
// `witnessPreVerified` controls L0Witness handling at L_0:
//
//   - false (direct observation): the helper verifies L0Witness against the
//     leader's pubKeyShare. On verify-fail, it fires Rule 5 against the
//     leader — sound because the bundle's outer envelope binds these bytes
//     to the leader.
//   - true (harvest from peer KindValue): the caller has ALREADY verified
//     L0Witness and the helper trusts it. Rule 5 is never fired from this
//     path — an emitter's forwarded L0Witness can't be cryptographically
//     attributed to the leader (the envelope binds it to the forwarder),
//     so the framing-the-leader attack would otherwise be open. See
//     [`docs/2abOBFT-REDESIGN-PLAN.md`](../../../../docs/2abOBFT-REDESIGN-PLAN.md)
//     §Op11 for the full attribution analysis.
//
// Caller invariants:
//
//   - `b` has passed ValidatePhase1Bundle (structural).
//   - If witnessPreVerified==true: caller verified L0Witness against the
//     leader's pubKey on b.Value BEFORE constructing the synthetic bundle.
//     A no-op witness (len==0) is treated as "no σ-pool contribution"
//     regardless of the flag.
func (i *Instance) retainPhase1Bundle(b *Phase1Bundle, observedOffset time.Duration, witnessPreVerified bool) {
	if i.retainedBundles[b.Layer] == nil {
		i.retainedBundles[b.Layer] = make(map[OperatorID][]*retainedBundle)
	}
	retained := i.retainedBundles[b.Layer][b.OperatorID]

	// Dedup against already-retained value_roots.
	for _, r := range retained {
		if bytes.Equal(r.Bundle.Value, b.Value) {
			// Identical re-broadcast — silent dedup.
			return
		}
	}

	// Distinct value_root.
	if len(retained) >= 2 {
		// Already have 2 distinct from this leader; drop the third+.
		return
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
	// pubKeyShare on V. On the direct path, verification gates pool
	// inclusion: a fake witness (leader signed garbage) fires Rule 5
	// against the leader and the σ-pool contribution is rejected; the
	// bundle's V is still retained for downstream pool semantics. On the
	// harvest path the caller has pre-verified, so we trust + pool
	// without re-verifying or firing Rule 5.
	if b.Layer == 0 && len(b.L0Witness) > 0 {
		if witnessPreVerified || i.verifyL0SigmaPartial(b.OperatorID, b.Value, b.L0Witness) {
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
}

// deepCopyBundle returns a defensive copy of b so retention state isn't
// affected by caller-owned slice mutation post-Observe.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	out := *b
	out.Value = append(Value{}, b.Value...)
	out.L0Witness = append(Signature{}, b.L0Witness...)
	return &out
}
