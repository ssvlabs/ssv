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

	// Look up the per-leader retention slot for this layer. Bundle dedup
	// against retained runs BEFORE VerifyPartial so that a re-delivery of
	// an already-retained V (e.g., the same leader bundle packed as a witness
	// in N peers' KindCommits, or normal gossipsub re-broadcast across mesh
	// paths) doesn't re-pay the BLS verify cost.
	//
	// Spec §Phase 1 line 154 lists "verify both signatures" first in the
	// validation order. We dedup first as a CPU optimization on the normal
	// hot path: byte-identical (op, V) means the same σ_V (signing is
	// deterministic) which already verified at first observation. The
	// optimization is correctness-safe (idempotent for any verified V); it
	// also bounds CPU cost under flood attacks where byz re-spams a single
	// valid bundle.
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
	// observation), saving redundant verifies on witness redelivery.
	leaderShare, ok := i.pubKeyShares[b.OperatorID]
	if !ok {
		// Leader's share not registered — treat as auth failure.
		return fmt.Errorf("obft: no pubkey share for leader %d", b.OperatorID)
	}
	if !i.signer.VerifyPartial(leaderShare, b.Value, b.SigmaV) {
		return fmt.Errorf("obft: phase-1 bundle σ_V does not verify against leader %d's share",
			b.OperatorID)
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
		//
		// Re-run the L_0 retroactive check: the second V_b can rescue
		// previously-deferred σ entries that signed V_b. Without this, an
		// honest peer whose σ on V_b arrived between the V_a and V_b
		// retentions stays in the deferred-Rule-5 pool until finalize, where
		// it would be incorrectly slashed.
		if b.Layer == 0 {
			i.reevaluateL0Sigmas()
		}
		return nil
	}

	// First retention for this (layer, leader). Stash a defensive deep copy
	// so retained bundle bytes are independent of caller-owned slices.
	i.bundles[b.Layer][b.OperatorID] = []*Phase1Bundle{deepCopyBundle(b)}

	// Retroactive cryptoFake check: any L_0 onion entries observed BEFORE any
	// V was retained at L_0 have not yet been Rule-5-checked (the check at
	// ObserveCommit gates on hasRetainedVAtL0). Re-evaluate now that a V is
	// retained — only fires Rule 5 for cryptoFake (unambiguous); unknownV
	// entries stay deferred until finalizeL0Rule5.
	if b.Layer == 0 {
		i.reevaluateL0Sigmas()
	}
	return nil
}

// reevaluateL0Sigmas re-runs the Rule 5 check on already-observed L_0 onion
// entries. Called when a Phase-1 bundle is first retained at L_0 to catch
// fake-σ entries from peers whose KindCommit arrived before the L_0 bundle.
//
// Only fires Rule 5 for the unambiguous cryptoFake case (partial fails verify
// on the claimed V). The "verifies, but V not currently retained" case is
// deferred to finalizeL0Rule5 at phase end — under leader equivocation a
// V we haven't retained yet may arrive later and rescue the entry.
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
	var snapshot []opEntries
	for op, entries := range i.peerOnions[0] {
		if op == leader {
			// The layer leader's σ_V is in bundles[0][leader], not
			// peerOnions; nothing to re-check.
			continue
		}
		// Defensive copy: detach from the underlying array so subsequent
		// removeOnionEntry mutations don't disturb our iteration.
		entriesCopy := make([]EncryptedLayer, len(entries))
		copy(entriesCopy, entries)
		snapshot = append(snapshot, opEntries{op: op, entries: entriesCopy})
	}
	for _, oe := range snapshot {
		for _, el := range oe.entries {
			if i.peerSigmaAtL0Verdict(oe.op, el) == l0SigmaCryptoFake {
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
			}
		}
	}
}

// finalizeL0Rule5 fires Rule 5 evidence on any L_0 σ entries whose claimed V
// was never retained over this Instance's lifetime. Called once via
// Instance.Finalize at slot end — must NOT run while further ObserveCommit /
// ObservePhase1Bundle calls may arrive, because late KindCommit witnesses
// can drive new L_0 retentions (ObservePhase1Bundle with observedOffset=0
// bypasses the T_commit window) that would rescue otherwise-unknownV entries.
// Calling from inside Resolve is unsafe — Resolve is polled by the runner.
//
// Without this final pass, an honest peer who signed a leader's V_b before
// V_b reached our retention (under leader equivocation, after we'd already
// retained V_a) would never be slashed when their V is genuinely fake — we'd
// have deferred the check at observe time and never re-fired.
func (i *Instance) finalizeL0Rule5() {
	if !i.hasRetainedVAtL0() {
		// No V retained at L_0 at all — every L_0 σ entry is inconclusive
		// (no V to compare against). Nothing to slash.
		return
	}
	type opEntries struct {
		op      OperatorID
		entries []EncryptedLayer
	}
	leader := i.cfg.Layers[0].Leader
	var snapshot []opEntries
	for op, entries := range i.peerOnions[0] {
		if op == leader {
			continue
		}
		entriesCopy := make([]EncryptedLayer, len(entries))
		copy(entriesCopy, entries)
		snapshot = append(snapshot, opEntries{op: op, entries: entriesCopy})
	}
	for _, oe := range snapshot {
		for _, el := range oe.entries {
			if i.peerSigmaAtL0Verdict(oe.op, el) == l0SigmaUnknownV {
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
			}
		}
	}
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
				SigmaV:    append(Signature{}, w.SigmaV...),
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
