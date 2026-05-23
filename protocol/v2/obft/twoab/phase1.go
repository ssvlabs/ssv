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
// Per spec §Phase 1: the bundle pairs `value` with the layer leader's σ
// partial on V at this `layer` (`LeaderSigma` field), signed via the
// V-keypair share. LeaderSigma binds V to the leader at the protocol layer
// (closing the withhold-then-fake-σ attack) and seeds `σ-pool[V_k]`
// (k = layer) with one partial from the moment the bundle is observed.
// The outer envelope's op-identity signature on the encoded bundle bytes
// is added by the SSV adapter at the wire layer.
//
// Side effects: LeaderSigma signing acquires the σ-side EKM lock at `layer`
// (`transitionToSigma(layer, value)`), so subsequent attempts to
// NR-direction at the same layer by the same leader op are blocked by EKM
// (the leader's Phase-2a entry at this layer stays consistent with the
// witness — see buildLayerEntry's σ-lock-aware branch). Self-pools the σ
// partial into σ-pool[layer][ValueRoot(value)] for the leader's own
// contribution. The leader σ-locks at fetch time (~1·BTT before the
// Phase-2a backstop), so a host flip of `value` post-fetch cannot be
// retracted (the crash-loss / re-org window the head-start trades for).
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
	if i.ended {
		return nil, ErrInstanceEnded
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

	// Sign LeaderSigma at this layer (every layer's leader carries a witness,
	// not just L_0). LeaderSigma = the leader's σ partial on `value` at
	// `layer`, seeding σ-pool[V_k] (k = layer) with a head-start partial.
	//
	// Sequencing: sign first, THEN acquire σ-lock + self-pool. A signer
	// failure (BLS infra issue) must NOT leave the EKM σ-locked without
	// a corresponding partial existing — that state would block any
	// further emission at this layer from this leader without a
	// recoverable retry path. By signing first (no state mutation on
	// failure), we ensure the leader can retry the build later if the
	// signer transiently fails.
	//
	// 1) Compute the partial. SignPartial is pure (no Instance state
	// mutation); if it errors, we abort with no side effects.
	partial, ok := i.ownPartials[layer]
	if !ok {
		p, err := i.signer.SignPartial(value)
		if err != nil {
			return nil, fmt.Errorf("twoab: sign LeaderSigma for leader at layer %d: %w", layer, err)
		}
		partial = p
	}
	// 2) Acquire σ-side EKM lock at `layer` on this value. Failure means
	// the leader has previously locked NR or σ on a different V at this
	// layer — should not happen for an honest leader who fetches once per
	// layer, but the impl enforces. We abort BEFORE caching the partial /
	// pooling, so a lock-fail leaves Instance state untouched (the signed
	// bytes are discarded).
	if err := i.transitionToSigma(layer, value); err != nil {
		return nil, fmt.Errorf("twoab: leader LeaderSigma σ-lock at layer %d: %w", layer, err)
	}
	// 3) Both sign and lock succeeded — commit the partial to cache and
	// self-pool into σ-pool[V_k] (k = layer). Idempotent on subsequent
	// calls with the same value (cache hit on ownPartials, idempotent
	// addToSigmaPool semantics).
	i.ownPartials[layer] = partial
	i.addToSigmaPool(layer, ValueRoot(value), i.ownOperatorID, partial)

	return &Phase1Bundle{
		ClusterID:   i.cfg.ClusterID,
		OperatorID:  i.ownOperatorID,
		Height:      i.cfg.Height,
		Layer:       layer,
		Value:       append(Value{}, value...), // defensive copy
		LeaderSigma: append(Signature{}, partial...),
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
//     (b) One or both retained via the harvest path (synthetic
//     bundle reconstructed from a peer KindValue): the outer
//     envelope is the *emitter's*, not the leader's — but the
//     LeaderSigma on each retained bundle is the leader's BLS σ
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
//     the upgrade. The upgrade KindValue carries the emitter's σ
//     partial inline, so the upgrade IS the σ-side terminal emission —
//     no separate σ-eligibility trigger follows.
func (i *Instance) ObservePhase1Bundle(b *Phase1Bundle, observedOffset time.Duration) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidatePhase1Bundle(b, i.cfg); err != nil {
		return err
	}
	// Direct-observation path: the bundle's outer envelope is op-identity-
	// signed by the claimed leader (the SSV adapter has verified this before
	// reaching the protocol layer). That cryptographic binding is what makes
	// Rule 5 attribution to the leader sound on LeaderSigma verify-fail —
	// hence witnessPreVerified=false (let retainPhase1Bundle verify and
	// fire Rule 5 if needed).
	i.retainPhase1Bundle(b, observedOffset, false /* witnessPreVerified */)
	return nil
}

// retainPhase1Bundle is the shared retention path for Phase-1 bundles,
// invoked by both ObservePhase1Bundle (direct observation) and the
// peer-harvest path in ObserveValueMsg (synthetic bundles reconstructed
// from a verified peer-KindValue forwarded witness).
//
// `witnessPreVerified` controls LeaderSigma handling (at the bundle's Layer):
//
//   - false (direct observation): the helper verifies LeaderSigma against the
//     leader's pubKeyShare. On verify-fail, it fires Rule 5 against the
//     leader at b.Layer — sound because the bundle's outer envelope binds
//     these bytes to the leader.
//   - true (harvest from peer KindValue): the caller has ALREADY verified
//     LeaderSigma and the helper trusts it. Rule 5 is never fired from this
//     path — an emitter's forwarded witness can't be cryptographically
//     attributed to the leader (the envelope binds it to the forwarder),
//     so the framing-the-leader attack would otherwise be open. See
//     [`docs/2abOBFT.md`](../../../../docs/2abOBFT.md)
//     §Slashing evidence (Rule 5) for the full attribution analysis. (The
//     harvest path only synthesizes L_0 bundles today, so
//     witnessPreVerified=true is L_0-only in practice; the code below is
//     already layer-general.)
//
// Caller invariants:
//
//   - `b` has passed ValidatePhase1Bundle (structural).
//   - If witnessPreVerified==true: caller verified LeaderSigma against the
//     leader's pubKey on b.Value BEFORE constructing the synthetic bundle.
//     A no-op witness (len==0) is treated as "no σ-pool contribution"
//     regardless of the flag.
func (i *Instance) retainPhase1Bundle(b *Phase1Bundle, observedOffset time.Duration, witnessPreVerified bool) {
	if i.retainedBundles[b.Layer] == nil {
		i.retainedBundles[b.Layer] = make(map[OperatorID][]*retainedBundle)
	}
	retained := i.retainedBundles[b.Layer][b.OperatorID]

	// Dedup against already-retained value_roots.
	//
	// Rule 5 (fake plaintext σ at L_0) fires only on the FIRST observed
	// bundle per (leader, V) — subsequent identical-V observations dedup
	// here without re-verifying the LeaderSigma. This is sound: BLS
	// signatures are deterministic, so a leader signing the same V twice
	// produces byte-identical LeaderSigma; a second observation with
	// matching V either matches the first LeaderSigma (no new information)
	// or differs (the byz emitted bytes-distinct trash for the same V,
	// which is structurally a separate fault — but since the protocol-
	// level evidence is keyed on V_root, dedup-on-V is the right
	// granularity).
	for _, r := range retained {
		if bytes.Equal(r.Bundle.Value, b.Value) {
			// Identical re-broadcast — silent dedup.
			return
		}
	}

	// Distinct value_root.
	if len(retained) >= MaxRetainedPerOpLayer {
		// Already have MaxRetainedPerOpLayer distinct from this leader;
		// drop the third+. Per spec §Phase 1 Retention bounds.
		return
	}

	copyB := deepCopyBundle(b)
	// Source derived from witnessPreVerified: harvest is the only path
	// that pre-verifies (envelope absent because synth is built from a
	// peer's KindValue). Direct path never pre-verifies (the bundle
	// carries the leader's envelope; this helper does the LeaderSigma
	// verify below).
	source := RetentionDirect
	if witnessPreVerified {
		source = RetentionHarvest
	}
	newEntry := &retainedBundle{
		Bundle:                 copyB,
		RetentionEstablishedAt: observedOffset,
		Source:                 source,
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
				SourceA: retained[0].Source,
				SourceB: source,
			},
		})
	} else {
		// First retention.
		i.retainedBundles[b.Layer][b.OperatorID] = []*retainedBundle{newEntry}
	}

	// LeaderSigma verification + σ-pool seeding (every layer, not just L_0).
	// The witness is the layer leader's σ partial on V, verifiable against
	// the leader's pubKeyShare on V (plaintext at every
	// layer — the deeper-layer witness is a head-start, NOT chained-
	// encrypted like Phase-2a σ entries). On the direct path, verification
	// gates pool inclusion: a fake witness (leader signed garbage) fires
	// Rule 5 against the leader at this layer and the σ-pool contribution is
	// rejected; the bundle's V is still retained for downstream pool
	// semantics. On the harvest path the caller has pre-verified, so we
	// trust + pool without re-verifying or firing Rule 5.
	if len(b.LeaderSigma) > 0 {
		if witnessPreVerified || i.verifySigmaPartial(b.OperatorID, b.Value, b.LeaderSigma) {
			i.addToSigmaPool(b.Layer, ValueRoot(b.Value), b.OperatorID, b.LeaderSigma)
			if b.Layer == 0 {
				// Cross-source Rule 3: the leader may have also σ-signed a
				// different V at L_0 via its own KindValue.L0Partial (or a
				// second witness). Re-check now, in any arrival order.
				i.maybeFireCrossSigmaV(b.OperatorID, b.Value)
			}
		} else if i.recordRule5(b.OperatorID, b.Layer) {
			i.recordEvidence(Evidence{
				Rule:       EvidenceFakePlaintextSigma,
				OperatorID: b.OperatorID,
				Layer:      b.Layer,
				FakePlaintextSigma: &FakePlaintextSigmaEvidence{
					OnionPartial:        append(Signature{}, b.LeaderSigma...),
					OnionValue:          append(Value{}, b.Value...),
					RetainedValueHashes: i.retainedValueHashes(b.Layer),
				},
			})
		}
	}

	// Per-tick processing cascade: a Phase-1 bundle arrival can unlock
	// the upgrade path (if this is V_0 arriving at a NoValue-path op
	// and host re-validates valid). The upgrade KindValue carries the
	// σ partial inline, so the upgrade IS the σ-side terminal emission.
	// A second arrival establishing equivocation may instead arm the
	// equivocation-trigger state for MaybeFirePhase2a. Run upgrade-first
	// then commit-trigger evaluation per §Emission ordering.
	i.afterStateDelta()
	// A retention change can flip computeLocalValueState into a fire-ready
	// state (a second distinct V → NRDirect, or a first V when host
	// validity is already recorded → Value). Signal L0Ready so the
	// runner/DES can async-fire MaybeFirePhase2a before TPhase2a.
	i.maybeSignalL0Ready()
}

// deepCopyBundle returns a defensive copy of b so retention state isn't
// affected by caller-owned slice mutation post-Observe.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	out := *b
	out.Value = append(Value{}, b.Value...)
	out.LeaderSigma = append(Signature{}, b.LeaderSigma...)
	return &out
}
