package base

import (
	"bytes"
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
//
// σ-lock is independent of host-validity within the Instance: the host's
// valid/not-valid verdict on V is recorded via ApplyHostValidity, which can
// happen BEFORE or AFTER BuildPhase1Bundle. Resolve does not consult host
// validity when assembling σ-pools — a leader who σ-locks via this method
// and then receives ApplyHostValidity(layer, V, false) remains σ-locked,
// and their σ_V contributes to V's σ-pool at Resolve time. This is a
// footgun for future host-callback integrations: production
// (Scheduler.processProposerSlot) hardcodes ApplyHostValidity(..., true)
// for the leader's own V immediately after BuildPhase1Bundle, side-stepping
// the issue. If you wire a real host validity check, ensure the host's
// verdict is consulted BEFORE BuildPhase1Bundle to avoid σ-locking on a
// host-invalid V.
func (i *Instance) BuildPhase1Bundle(layer int, value Value) (*Phase1Bundle, error) {
	if i == nil {
		return nil, fmt.Errorf("obft: nil instance")
	}
	if i.ended {
		return nil, ErrInstanceEnded
	}
	if layer < 0 || layer >= i.cfg.K() {
		return nil, fmt.Errorf("%w: layer %d outside [0, %d)", ErrLayerOutOfRange, layer, i.cfg.K())
	}
	if i.cfg.Layers[layer].Leader != i.ownOperatorID {
		return nil, fmt.Errorf("%w: local operator %d is not leader at layer %d (leader is %d)",
			ErrNotLeader, i.ownOperatorID, layer, i.cfg.Layers[layer].Leader)
	}
	if len(value) == 0 {
		return nil, ErrEmptyValue
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
		ClusterID:   i.cfg.ClusterID,
		OperatorID:  i.ownOperatorID,
		Height:      i.cfg.Height,
		Layer:       layer,
		Value:       append(Value{}, value...),
		LeaderSigma: partial,
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

	if layer == 0 {
		i.maybeSignalL0Ready()
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
	if i == nil {
		return fmt.Errorf("obft: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidatePhase1Bundle(b, i.cfg); err != nil {
		return err
	}
	if !i.observedTimeOK(observedOffset) {
		return ErrLatePhase1Bundle
	}

	// Look up the per-leader retention slot for this layer. Bundle dedup
	// against retained V's runs before VerifyPartial as a CPU optimization
	// on the normal hot path — gossipsub re-broadcast across mesh paths
	// causes repeat observations of the same (op, V) at the receiver.
	// (Witness rehydration USED to be a second source of repeat observations
	// when LeaderSigmaWitness carried full V, but witnesses now ship
	// value_root only and don't trigger ObservePhase1Bundle anymore.)
	//
	// Spec §Phase 1 / "Protocol-level checks" enumerates "verify both
	// signatures + check first-observation timestamp" as the receiver-side
	// checks; the spec is silent on the ordering of dedup vs verify within
	// those checks.
	// Dedup-first is semantically equivalent because a byte-identical (op, V)
	// re-observation must carry the same σ_V (deterministic signing) which
	// already verified at first observation. Every distinct V is still
	// BLS-verified before retention. No invalid σ_V can enter via the dedup
	// path (matches only against already-verified retained V's).
	if i.bundles[b.Layer] == nil {
		i.bundles[b.Layer] = make(map[OperatorID][]*Phase1Bundle)
	}
	retained := i.bundles[b.Layer][b.OperatorID]
	for _, r := range retained {
		if bytes.Equal(r.Value, b.Value) {
			// Same value_root already retained from a previously-verified
			// bundle — drop silently. The canonical entry is whichever
			// arrived first and verified.
			return nil
		}
	}

	// Cryptographic-auth check on σ_V against the leader's pubkey share.
	// Runs only when retention would actually take effect (new V or first
	// observation), saving redundant verifies on gossipsub re-broadcast.
	leaderShare, ok := i.pubKeyShares[b.OperatorID]
	if !ok {
		// Leader's share not registered — treat as auth failure.
		return fmt.Errorf("obft: no pubkey share for leader %d", b.OperatorID)
	}
	if !i.signer.VerifyPartial(leaderShare, b.Value, b.LeaderSigma) {
		return fmt.Errorf("obft: phase-1 bundle σ_V does not verify against leader %d's share",
			b.OperatorID)
	}
	// F1: cache the verified result so Resolve's leader-bundle σ-walk skips
	// the redundant re-verify. See verifiedPartials field doc-comment.
	i.markVerified(b.OperatorID, b.Layer, b.LeaderSigma)

	// Distinct value_root. Cap retention per the shared retention bound.
	if len(retained) >= MaxRetainedPerOpLayer {
		// Already have MaxRetainedPerOpLayer distinct from this leader; drop
		// the third (and beyond). The first two are sufficient evidence.
		return nil
	}

	// Rule 2 (leader equivocation) detection across all source combinations:
	// the new bundle's V vs any existing distinct V from this leader, sourced
	// from either a prior retained bundle (bundle-vs-bundle, the historical
	// case) or witnessedLeaderSigma (bundle-vs-witness, when a peer's
	// witness for an equivocated V arrived before the bundle for the other
	// V did). Dedup'd via recordRule2 so the symmetric path in harvestWitness
	// doesn't double-fire.
	//
	// Local-state effect of equivocation per spec §Phase 1 / Equivocation
	// handling: at T_commit, an operator with ≥ 2 distinct V's retained
	// emits NR (per the equivocation rule, no winner-picking under f=1
	// byzantine). Pre-T_commit, leave the state alone — chosenVForLayer
	// will return false (≥ 2 retained = no unique V) so BuildOwnCommit
	// will commit NR for this layer at T_commit. If the operator already
	// σ-locked on the first V (byzantine delivered it before observing
	// equivocation), they stay σ-locked per cross-phase exclusivity.
	copyB := deepCopyBundle(b)
	if other := i.findExistingLeaderSigmaOnDistinctV(b.Layer, b.OperatorID, b.Value); other != nil && i.recordRule2(b.OperatorID, b.Layer) {
		i.recordEvidence(Evidence{
			Rule:       EvidenceLeaderEquivocation,
			OperatorID: b.OperatorID,
			Layer:      b.Layer,
			LeaderEquivocation: &LeaderEquivocationEvidence{
				BundleA: other,
				BundleB: copyB,
			},
		})
	}
	i.bundles[b.Layer][b.OperatorID] = append(retained, copyB)

	// Rule 1 (cross-signing) order-independence — leader-specific case.
	// Spec §Cross-signing detection: "σ from Phase 1 + NR/NV from Phase 2"
	// pairs a Phase-1 σ_V with an NR partial on the same layer from the same
	// operator. The symmetric NR-side check in ObserveCommit fires when an NR
	// partial arrives after the bundle was retained; this branch fires when
	// the byzantine leader's KindCommit (with NR at their own layer) was
	// observed first and the bundle is arriving now. Spec detection is
	// "Immediate (dual partials on the wire)" — order-independent.
	if nrSig, hasNR := i.peerNR[b.Layer][b.OperatorID]; hasNR && i.recordRule1(b.OperatorID, b.Layer) {
		i.recordEvidence(Evidence{
			Rule:       EvidenceCrossSigning,
			OperatorID: b.OperatorID,
			Layer:      b.Layer,
			CrossSigning: &CrossSigningEvidence{
				SigmaPartial: append(Signature{}, b.LeaderSigma...),
				SigmaValue:   append(Value{}, b.Value...),
				NRPartial:    append(Signature{}, nrSig...),
			},
		})
	}

	// Retroactive L_0 evidence checks: any L_0 onion entries observed BEFORE
	// any V was retained had verdict l0SigmaInconclusive (couldn't evaluate
	// Rule 5 without a retained V). Re-evaluate now that a V is retained:
	//   - Rule 5 cryptoFake (partial fails op's own pubshare on claimed V).
	//   - Rule 5 unknownV (partial verifies on op's claimed V but V not in
	//     retained set). Dedup'd via recordRule5UnknownV across this
	//     retroactive path and ObserveCommit's forward-order path.
	//   - Rule 3 retroactive for the L_0 leader's own onion entry when its
	//     V differs from the retained bundle V — see reevaluateL0Sigmas.
	if b.Layer == 0 {
		i.reevaluateL0Sigmas()
		i.maybeSignalL0Ready()
	}

	// Retroactive deeper-layer (k > 0) Rule 3 leader-vs-bundle cross-V check:
	// if the leader's own onion entry at this layer was already in peerOnions
	// when the bundle arrived, the forward-order path in ObserveCommit hadn't
	// yet seen the bundle so didn't fire. Now that the bundle is retained,
	// check for cross-V. (L_0 is handled by reevaluateL0Sigmas above; this
	// branch is the k > 0 twin.) Dedup via recordRule3Leader.
	if b.Layer > 0 {
		for _, el := range i.peerOnions[b.Layer][b.OperatorID] {
			i.checkLeaderBundleCrossV(b.OperatorID, b.Layer, el.Value, el.Ciphertext)
		}
	}
	return nil
}

// reevaluateL0Sigmas re-runs retroactive evidence checks on already-observed
// L_0 onion entries. Called when a Phase-1 bundle is first (or second)
// retained at L_0 to catch:
//
//   - Rule 5 (FakePlaintextSigma, cryptoFake variant) for peer (non-leader)
//     onion entries whose σ doesn't verify against the op's pubshare on the
//     claimed V. Unambiguous byzantine; fires immediately and removes the
//     offending entry from peerOnions.
//   - Rule 5 (FakePlaintextSigma, unknownV variant) for peer (non-leader)
//     onion entries that verify on the op's claimed V but whose V doesn't
//     match any retained V. Fires per spec MUST-log framing; dedup'd via
//     recordRule5UnknownV across this retroactive path and the forward-
//     order path in ObserveCommit. Entry stays in peerOnions so the partial
//     can still contribute if V later becomes known.
//   - Rule 3 (CrossOnionEquivocation) for the L_0 leader's own onion entry
//     where its V differs from a retained Phase-1 bundle V — order-independent
//     twin of the ObserveCommit check at the σ-side branch. Per spec
//     §Cross-onion partial-sig equivocation, detection is "Immediate (two
//     σ partials on different V)".
//
// Snapshot semantics: removeOnionEntry rewrites the underlying slice
// in-place (`out := entries[:0]`); iterating the same slice header while
// the backing array mutates would yield stale or shifted reads. We snapshot
// (op, []EncryptedLayer) tuples up-front and iterate only the snapshot.
func (i *Instance) reevaluateL0Sigmas() {
	type opEntries struct {
		op      OperatorID
		entries []EncryptedLayer
	}
	leader := i.cfg.Layers[0].Leader
	var peerSnapshot []opEntries
	var leaderEntries []EncryptedLayer
	for op, entries := range i.peerOnions[0] {
		// Defensive copy: detach from the underlying array so subsequent
		// removeOnionEntry mutations don't disturb our iteration.
		entriesCopy := make([]EncryptedLayer, len(entries))
		copy(entriesCopy, entries)
		if op == leader {
			leaderEntries = entriesCopy
			continue
		}
		peerSnapshot = append(peerSnapshot, opEntries{op: op, entries: entriesCopy})
	}
	for _, oe := range peerSnapshot {
		for _, el := range oe.entries {
			switch i.peerSigmaAtL0Verdict(oe.op, el) {
			case l0SigmaCryptoFake:
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakePlaintextSigma,
					OperatorID: oe.op,
					Layer:      0,
					FakePlaintextSigma: &FakePlaintextSigmaEvidence{
						OnionPartial:        append(Signature{}, el.Ciphertext...),
						OnionValue:          append(Value{}, el.Value...),
						RetainedValueHashes: i.retainedL0ValueHashes(),
					},
				})
				i.removeOnionEntry(0, oe.op, &el)
			case l0SigmaUnknownV:
				// Retroactive Rule 5 unknownV fire: entry was observed
				// pre-bundle (inconclusive at observe time), now V is
				// retained but doesn't match this entry's V. Per spec
				// Rule 5, fire — see the matching forward-order path in
				// phase2.go ObserveCommit. Dedup via rule5UnknownVFired
				// across both paths. Entry stays in peerOnions (partial
				// might still contribute if its V becomes known later).
				if i.recordRule5UnknownV(oe.op, 0) {
					i.recordEvidence(Evidence{
						Rule:       EvidenceFakePlaintextSigma,
						OperatorID: oe.op,
						Layer:      0,
						FakePlaintextSigma: &FakePlaintextSigmaEvidence{
							OnionPartial:        append(Signature{}, el.Ciphertext...),
							OnionValue:          append(Value{}, el.Value...),
							RetainedValueHashes: i.retainedL0ValueHashes(),
						},
					})
				}
			}
		}
	}
	// Rule 3 retroactive check for the L_0 leader's own onion entry. The
	// ObserveCommit σ-side branch already fires Rule 3 when the bundle was
	// retained BEFORE the leader's L_0 onion arrived; this handles the
	// reverse order (onion first, bundle now). Dedup via the per-(op, layer)
	// Rule 3 fire-set so the order-flipped twin doesn't double-record.
	// Shares checkLeaderBundleCrossV with the deeper-layer twin (k > 0)
	// triggered from ObservePhase1Bundle.
	for _, el := range leaderEntries {
		i.checkLeaderBundleCrossV(leader, 0, el.Value, el.Ciphertext)
	}
}

// deepCopyBundle returns a deep copy of b — the byte slices inside (Value,
// LeaderSigma) are independent of the source. Used at retention boundaries so
// caller-owned slices can be modified without corrupting Instance state.
//
// Style: struct-literal + slice-field overrides (matching the twoab
// sibling helper). Harder to forget a slice field than the imperative
// field-by-field literal form; new scalar fields added to Phase1Bundle
// are picked up automatically by the `*b` copy, only slice/pointer
// fields need explicit overrides below.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	out := *b
	out.Value = append(Value{}, b.Value...)
	out.LeaderSigma = append(Signature{}, b.LeaderSigma...)
	return &out
}

// deepCopyCommit returns a deep copy of c — every nested slice (Layers,
// NRPartials, Witnesses) and their byte fields are independent of the source.
// Used when retaining a peer's first observed Commit for top-level Rule 3
// evidence packaging (CommitEquivocationEvidence).
func deepCopyCommit(c *Commit) *Commit {
	out := &Commit{
		ClusterID:  c.ClusterID,
		OperatorID: c.OperatorID,
		Height:     c.Height,
	}
	if len(c.Layers) > 0 {
		out.Layers = make([]EncryptedLayer, len(c.Layers))
		for i, el := range c.Layers {
			out.Layers[i] = EncryptedLayer{
				Value:      append(Value{}, el.Value...),
				Ciphertext: append([]byte{}, el.Ciphertext...),
			}
		}
	}
	if len(c.NRPartials) > 0 {
		out.NRPartials = make([]NRPartial, len(c.NRPartials))
		for i, p := range c.NRPartials {
			out.NRPartials[i] = NRPartial{
				Layer:      p.Layer,
				PartialSig: append(Signature{}, p.PartialSig...),
			}
		}
	}
	if len(c.Witnesses) > 0 {
		out.Witnesses = make([]LeaderSigmaWitness, len(c.Witnesses))
		for i, w := range c.Witnesses {
			out.Witnesses[i] = LeaderSigmaWitness{
				Layer:     w.Layer,
				Leader:    w.Leader,
				ValueRoot: w.ValueRoot,
				Sigma:     append(Signature{}, w.Sigma...),
			}
		}
	}
	return out
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
//
// Divergence from twoab: base REJECTS a verdict-flip on the same
// (layer, V) with an error (the host is expected to lock at Phase 1
// against a stable head). twoab tolerates verdict-flip silently — it
// treats host validity as a flowing signal that can shift mid-slot.
// See docs/OBFT-TWOAB-CONVERGENCE-PLAN.md §C4.
func (i *Instance) ApplyHostValidity(layer int, value Value, valid bool) error {
	if i == nil {
		return fmt.Errorf("obft: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if layer < 0 || layer >= i.cfg.K() {
		return fmt.Errorf("%w: layer %d outside [0, %d)", ErrLayerOutOfRange, layer, i.cfg.K())
	}
	if len(value) == 0 {
		return ErrEmptyValue
	}
	if i.hostVerdict[layer] == nil {
		i.hostVerdict[layer] = make(map[[32]byte]bool)
	}
	key := ValueRoot(value)
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
	if layer == 0 {
		i.maybeSignalL0Ready()
	}
	return nil
}

// chosenVForLayer returns the operator's σ-target V at `layer` if uniquely
// determined.
//
// Primary path (Phase-1 retention): exactly one retained Phase-1 bundle at
// the layer's designated leader AND host validated it.
//
// Peer-reflood-V path (spec §Phase 2 / Peer-reflood V via early commit):
// no Phase-1 bundle retained locally, but a uniquely-observed V appears
// across peer σ-onion entries at this layer (peerOnions[layer]) AND that
// V has a verified leader σ_L^V (witnessedLeaderSigma[layer][ValueRoot(V)])
// AND host validated it. The witnessed-σ_L^V gate is byz-safe: a peer
// fabricating V_fake cannot trick honest receivers without a valid σ_L^V
// from the leader on that V.
//
// Returns (nil, false) if cluster-observable equivocation (≥ 2 distinct
// V's across bundles ∪ peerOnions ∪ witnessedLeaderSigma → cross-phase
// exclusivity routes through NR regardless of which source first exposed
// the second V), no retained or peer-observed V, or host hasn't validated.
func (i *Instance) chosenVForLayer(layer int) (Value, bool) {
	// Cross-phase-exclusivity equivocation gate: cluster-observable
	// equivocation at this layer forces NR regardless of which source
	// exposed each V. Without this, an operator with bundle V_a +
	// peer-V V_b could σ on V_a despite the cluster having attributable
	// Rule 2 evidence on the leader's equivocation.
	if i.distinctVCountAtLayer(layer) >= 2 {
		return nil, false
	}

	leaderMap := i.bundles[layer]
	expectedLeader := i.cfg.Layers[layer].Leader
	retained := leaderMap[expectedLeader]
	if len(retained) == 1 {
		// Primary path: uniquely-retained Phase-1 bundle.
		return checkHostValidV(i.hostVerdict[layer], retained[0].Value)
	}

	// Peer-reflood-V path: no retained bundle. Use a uniquely-observed
	// peer-V provided it has a verified leader σ_L^V.
	v, ok := i.uniquePeerV(layer)
	if !ok {
		return nil, false
	}
	if _, witnessed := i.witnessedLeaderSigma[layer][ValueRoot(v)]; !witnessed {
		return nil, false
	}
	return checkHostValidV(i.hostVerdict[layer], v)
}

// checkHostValidV returns (v, true) if the host has recorded `v` as valid
// at this layer's verdict map; (nil, false) otherwise (not-recorded or
// recorded-not-valid).
func checkHostValidV(verdicts map[[32]byte]bool, v Value) (Value, bool) {
	if verdicts == nil {
		return nil, false
	}
	valid, recorded := verdicts[ValueRoot(v)]
	if !recorded || !valid {
		return nil, false
	}
	return v, true
}
