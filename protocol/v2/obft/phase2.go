package obft

import (
	"bytes"
	"fmt"
)

// BuildOwnOnion builds the local operator's KindOnion in its current state.
//
// Per spec §Phase 2 / Sub-phasing, this method may be called multiple times
// during [TCommit, TCommit + Delta2] as σ-eligibility transitions late
// (e.g., late re-flood delivers V to a previously-Defer-state operator).
// Each call returns the current σ-side state — the caller broadcasts whatever
// changed since their last call (gossipsub naturally dedups identical bytes).
//
// Layers where the operator is σ-eligible (host-validated V is uniquely
// retained, no equivocation observed, no prior NR-lock) get a non-empty
// EncryptedLayer; other layers get the empty entry.
//
// EKM enforcement: σ-emission locks sigmaLocked[layer] on the chosen V.
// Subsequent calls produce the same partial (cached) so repeat broadcasts
// are byte-identical.
func (i *Instance) BuildOwnOnion() (*Onion, error) {
	K := i.cfg.K()
	layers := make([]EncryptedLayer, K)

	for k := 0; k < K; k++ {
		// Layer's leader is special: their Phase-1 σ_V is their σ-side
		// commitment, not their Onion entry. Skip Onion-σ-emit at the
		// layer they lead — Phase-1 was already their cross-phase σ-side.
		// (BuildPhase1Bundle sets sigmaLocked at the leader's layer; the
		// chosenVForLayer path below would also cover this if the bundle
		// has been observed locally + host-validated, but skipping is
		// simpler and avoids double-emission of the same partial.)
		if i.cfg.Layers[k].Leader == i.ownOperatorID {
			continue
		}

		// σ-eligibility check.
		v, ok := i.chosenVForLayer(k)
		if !ok {
			continue
		}
		// Cross-phase / single-σ-V locking (idempotent on same V).
		if err := i.transitionToSigma(k, v); err != nil {
			// Either NR-locked or equivocation-locked — skip σ-emit at
			// this layer.
			continue
		}

		partial, ok := i.ownPartials[k]
		if !ok {
			var err error
			partial, err = i.signer.SignPartial(v)
			if err != nil {
				return nil, fmt.Errorf("obft: sign σ at layer %d: %w", k, err)
			}
			i.ownPartials[k] = partial
		}

		ct, err := i.chainEncryptForLayer(k, partial)
		if err != nil {
			return nil, fmt.Errorf("obft: encrypt layer %d: %w", k, err)
		}
		layers[k] = EncryptedLayer{
			Value:      append(Value{}, v...),
			Ciphertext: ct,
		}
	}

	return &Onion{
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layers:     layers,
	}, nil
}

// ObserveOnion records a peer's KindOnion. Per spec §Phase 2 / Wire format,
// KindOnion may be emitted multiple times per (operator, slot); receivers
// track per-(operator, layer) σ-presence cumulatively.
//
// Per spec §Phase 1 / Validity-gate at L_0:
//   - At L_0 with retained V: a peer's plaintext σ partial that does not
//     verify against any retained V does NOT count as σ-emit observed.
//     The auth-signed Onion entry is recorded as Rule 5 evidence (fake
//     plaintext σ at L_0).
//   - At L_0 without retained V: any auth-signed Onion claiming σ at L_0
//     counts as σ-emit observed (encrypted-presence-equivalent fallback,
//     to preserve Defer-due-to-partition recovery).
//
// At deeper layers (k > 0), σ partials are encrypted; encrypted-presence
// alone counts as σ-emit observed. Decryption (and possible Rule 4
// detection) happens at Phase 3 reconstruction time.
func (i *Instance) ObserveOnion(o *Onion) error {
	if err := ValidateOnion(o, i.cfg); err != nil {
		return err
	}
	K := i.cfg.K()

	for k := 0; k < K; k++ {
		el := o.Layers[k]
		if len(el.Value) == 0 || len(el.Ciphertext) == 0 {
			continue // operator did not contribute at this layer
		}

		// Track the entry per (layer, operator). Up to 2 distinct entries
		// retained for cross-onion equivocation evidence (Rule 3).
		if i.peerOnions[k] == nil {
			i.peerOnions[k] = make(map[OperatorID][]EncryptedLayer)
		}
		existing := i.peerOnions[k][o.OperatorID]

		// Find an existing entry with the same value.
		seen := false
		for _, e := range existing {
			if bytes.Equal(e.Value, el.Value) {
				// Same value already retained from this operator — drop
				// (an honest operator emits the same partial across
				// repeat KindOnions; the cached one is canonical).
				seen = true
				break
			}
		}
		if seen {
			continue
		}

		// Distinct value at the same (layer, operator). If we already had
		// one, this is cross-onion equivocation (Rule 3).
		if len(existing) >= 1 {
			if len(existing) >= 2 {
				// Cap at 2 distinct; further distinct entries are dropped.
				continue
			}
			// Record evidence pairing the two distinct entries.
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossOnionEquivocation,
				OperatorID: o.OperatorID,
				Layer:      k,
				CrossOnionEquivocation: &CrossOnionEquivocationEvidence{
					ValueA:   existing[0].Value,
					ValueB:   el.Value,
					PartialA: existing[0].Ciphertext, // for L_0 == σ partial; for k>0 ciphertext (decoded later)
					PartialB: el.Ciphertext,
				},
			})
		}

		// Append. Defensive copies for slices retained beyond this call.
		entryCopy := EncryptedLayer{
			Value:      append(Value{}, el.Value...),
			Ciphertext: append([]byte{}, el.Ciphertext...),
		}
		i.peerOnions[k][o.OperatorID] = append(existing, entryCopy)

		// L_0 specific: validity-gate on plaintext σ partial.
		if k == 0 {
			if i.peerSigmaAtL0Verifies(o.OperatorID, el) {
				// Counts as observed; nothing extra to do — the entry is
				// retained and Resolve will pick it up.
			} else if i.hasRetainedVAtL0() {
				// Receiver has retained V at L_0 but partial doesn't
				// verify against any retained V — Rule 5 evidence.
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakePlaintextSigma,
					OperatorID: o.OperatorID,
					Layer:      0,
					FakePlaintextSigma: &FakePlaintextSigmaEvidence{
						OnionPartial:        append(Signature{}, el.Ciphertext...),
						OnionValue:          append(Value{}, el.Value...),
						RetainedValueHashes: i.retainedL0ValueHashes(),
					},
				})
				// Strip the entry from the peerOnions retention so it
				// does not contribute to σ-pool. (Keeping it in peerOnions
				// for cross-onion equivocation already happened above
				// before this branch; remove just the most recently added
				// entry to ensure it doesn't enter σ-pool.)
				i.removeOnionEntry(k, o.OperatorID, &el)
			}
			// If !hasRetainedVAtL0 and verify failed → no retained V means
			// we couldn't verify against anything. Fall back to
			// encrypted-presence rule: the entry counts as σ-emit observed
			// (preserves Defer-partition recovery). No evidence yet —
			// once V arrives we may re-verify and downgrade. This is
			// best-effort; the spec's MUST-gossip rule for Rule 5 is what
			// closes this attribution gap across the cluster.
		}

		// Cross-signing detection (Rule 1): σ at this layer + NR at this
		// layer from same operator?
		if i.peerNR[k] != nil {
			if nrSig, hasNR := i.peerNR[k][o.OperatorID]; hasNR {
				i.recordEvidence(Evidence{
					Rule:       EvidenceCrossSigning,
					OperatorID: o.OperatorID,
					Layer:      k,
					CrossSigning: &CrossSigningEvidence{
						SigmaPartial: append(Signature{}, el.Ciphertext...),
						SigmaValue:   append(Value{}, el.Value...),
						NRPartial:    append(Signature{}, nrSig...),
					},
				})
			}
		}
	}
	return nil
}

// peerSigmaAtL0Verifies reports whether peer `op`'s plaintext σ partial at
// L_0 (entry `el`) verifies against any retained V at L_0.
//
// At L_0, el.Ciphertext is the plaintext σ partial. We verify it against
// the operator's own pubkey share (the partial is signed by their V-share),
// using el.Value as the signed message.
//
// Note: el.Value is what `op` claims to have signed; if it matches a retained
// V at L_0, then verifying the partial against (op's pub-share, el.Value)
// confirms `op` actually signed that retained V.
func (i *Instance) peerSigmaAtL0Verifies(op OperatorID, el EncryptedLayer) bool {
	leaderMap := i.bundles[0]
	if len(leaderMap) == 0 {
		return false
	}
	pubShare, ok := i.pubKeyShares[op]
	if !ok {
		return false
	}
	// Check that el.Value matches some retained V at L_0.
	matchesRetained := false
	for _, retained := range leaderMap {
		for _, b := range retained {
			if bytes.Equal(b.Value, el.Value) {
				matchesRetained = true
				break
			}
		}
		if matchesRetained {
			break
		}
	}
	if !matchesRetained {
		return false
	}
	// Verify the partial against op's V-share pubkey on el.Value.
	return i.signer.VerifyPartial(pubShare, el.Value, el.Ciphertext)
}

// hasRetainedVAtL0 reports whether any Phase-1 bundle has been retained at L_0.
func (i *Instance) hasRetainedVAtL0() bool {
	for _, retained := range i.bundles[0] {
		if len(retained) > 0 {
			return true
		}
	}
	return false
}

// retainedL0ValueHashes returns the value_root hashes of all retained V's
// at L_0 (for inclusion in Rule 5 evidence so verifiers can reproduce the
// partial-vs-V check).
func (i *Instance) retainedL0ValueHashes() [][]byte {
	var out [][]byte
	for _, retained := range i.bundles[0] {
		for _, b := range retained {
			h := []byte(valueRootKey(b.Value))
			out = append(out, h)
		}
	}
	return out
}

// removeOnionEntry removes the matching peerOnions entry for (layer, op).
// Called after detecting Rule 5 to keep fake-σ entries out of the σ-pool.
func (i *Instance) removeOnionEntry(layer int, op OperatorID, target *EncryptedLayer) {
	entries := i.peerOnions[layer][op]
	out := entries[:0]
	for _, e := range entries {
		if bytes.Equal(e.Value, target.Value) && bytes.Equal(e.Ciphertext, target.Ciphertext) {
			continue
		}
		out = append(out, e)
	}
	i.peerOnions[layer][op] = out
}

// BuildOwnNR builds the local operator's KindNR at end of Phase 2. Per spec
// §Phase 2, this is emitted at most once per (slot, operator) at TCommit +
// Delta2.
//
// Carries NR partials for all layers in [0, K-1) where the operator is
// NR-committed (NR-silent or NV) per local state. Layers where the operator
// is σ-committed contribute no NR partial (cross-phase exclusivity).
//
// EKM enforcement: each NR-emit locks nrLocked[layer]. Subsequent calls
// produce byte-identical output.
//
// Caller must invoke PhaseTwoEnd before BuildOwnNR so the force-commit rule
// has applied to all Defer layers.
func (i *Instance) BuildOwnNR() (*NR, error) {
	if !i.phaseTwoEnded {
		return nil, fmt.Errorf("obft: BuildOwnNR called before PhaseTwoEnd")
	}
	K := i.cfg.K()
	var partials []NRPartial

	for k := 0; k < K-1; k++ {
		// NR is emitted only if the operator's local state is NR-side at
		// this layer (and not σ-locked).
		st := i.localState[k]
		if st != CommitNRSilent && st != CommitNV {
			continue
		}
		if err := i.transitionToNR(k, st); err != nil {
			// σ-locked at this layer — should not happen given the state
			// check above, but defensively skip rather than corrupt the
			// EKM log.
			continue
		}

		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
		sig, err := i.tagSigner.SignPartial(tag)
		if err != nil {
			return nil, fmt.Errorf("obft: sign NR partial at layer %d: %w", k, err)
		}
		partials = append(partials, NRPartial{
			Layer:      k,
			PartialSig: sig,
		})
	}
	return &NR{
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Partials:   partials,
	}, nil
}

// ObserveNR records a peer's KindNR. Per spec, validates each per-layer NR
// partial against the operator's IBE pubkey share when the share map is
// available (Option B); under Option A no separate IBE polynomial exists,
// and per-partial verification falls back to "store and verify-on-aggregate".
func (i *Instance) ObserveNR(nr *NR) error {
	if err := ValidateNR(nr, i.cfg); err != nil {
		return err
	}

	for _, p := range nr.Partials {
		if i.ibePubKeyShares != nil {
			pubShare, ok := i.ibePubKeyShares[nr.OperatorID]
			if !ok {
				return fmt.Errorf("obft: no IBE pubkey share for operator %d", nr.OperatorID)
			}
			tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, p.Layer)
			if !i.tagSigner.VerifyPartial(pubShare, tag, p.PartialSig) {
				return fmt.Errorf("obft: NR partial from op %d at layer %d failed verification",
					nr.OperatorID, p.Layer)
			}
		}
		if i.peerNR[p.Layer] == nil {
			i.peerNR[p.Layer] = make(map[OperatorID]Signature)
		}
		// Idempotent on duplicate observation.
		if _, exists := i.peerNR[p.Layer][nr.OperatorID]; exists {
			continue
		}
		i.peerNR[p.Layer][nr.OperatorID] = append(Signature{}, p.PartialSig...)

		// Rule 1 — cross-signing detection: did this operator already have
		// a σ entry at this layer?
		if onionEntries := i.peerOnions[p.Layer][nr.OperatorID]; len(onionEntries) > 0 {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossSigning,
				OperatorID: nr.OperatorID,
				Layer:      p.Layer,
				CrossSigning: &CrossSigningEvidence{
					SigmaPartial: append(Signature{}, onionEntries[0].Ciphertext...),
					SigmaValue:   append(Value{}, onionEntries[0].Value...),
					NRPartial:    append(Signature{}, p.PartialSig...),
				},
			})
		}
	}
	return nil
}

// PhaseTwoEnd applies the end-of-Phase-2 force-commit rule (spec §Phase 2 /
// Operator commitments). Must be called exactly once at TCommit + Delta2,
// before BuildOwnNR.
//
// For each layer in [0, K-1) where the local operator is in a Defer state:
//
//   - Defer-due-to-partition: if V has been received by now AND host validates
//     AND no equivocation observed → transition to σ; else NR (silent-leader).
//   - Defer-due-to-equivocation: NR (silent-leader rule applies).
//   - Undecided (no peer σ-emit observed, no V received): NR (silent-leader).
//   - NV: stays NV (will emit NR partial as NV is operationally NR).
//
// The deepest layer (K-1) has no NR tag; it is left at whatever state it's in
// (σ if eligible, else effectively "no contribution").
func (i *Instance) PhaseTwoEnd() error {
	if i.phaseTwoEnded {
		return nil
	}
	i.phaseTwoEnded = true

	K := i.cfg.K()
	for k := 0; k < K; k++ {
		// Already σ-locked or NR-locked → no force needed.
		if i.sigmaLocked[k] || i.nrLocked[k] {
			continue
		}

		// Try σ-side first: if Defer-partition resolved (V retained, host
		// validated, no equivocation), σ-emit. This may also cover plain
		// "I just hadn't gotten to BuildOwnOnion before now" cases.
		if v, ok := i.chosenVForLayer(k); ok {
			if err := i.transitionToSigma(k, v); err == nil {
				// Pre-sign the partial so a subsequent BuildOwnOnion call
				// returns a populated entry without re-signing surprise.
				if _, cached := i.ownPartials[k]; !cached {
					sig, err := i.signer.SignPartial(v)
					if err != nil {
						return fmt.Errorf("obft: late σ-sign at layer %d: %w", k, err)
					}
					i.ownPartials[k] = sig
				}
				continue
			}
			// transitionToSigma failed (e.g., equivocation-locked). Fall
			// through to NR.
		}

		// NR-side at layers with an NR tag (k < K-1).
		if k < K-1 {
			// Reason for NR is informational: NV if host returned
			// not-valid for the operator's chosen V; else NR-silent.
			target := CommitNRSilent
			if v, retained := i.chosenVAtLayerForNVCheck(k); retained {
				if verdicts := i.hostVerdict[k]; verdicts != nil {
					if v2, recorded := verdicts[valueRootKey(v)]; recorded && !v2 {
						target = CommitNV
					}
				}
			}
			// For Defer-due-to-equivocation, the spec says force-NR with
			// NR-silent label (it's not host-validity related).
			if i.localState[k] == CommitDeferEquivocation {
				target = CommitNRSilent
			}
			if err := i.transitionToNR(k, target); err != nil {
				return fmt.Errorf("obft: end-of-Phase-2 NR at layer %d: %w", k, err)
			}
		}
		// k == K-1: no NR tag, no force-commit needed. State stays whatever
		// it was (probably Undecided or Defer-partition); this layer just
		// doesn't contribute to the cluster's NR-pool (there's no "next
		// layer" to advance to).
	}
	return nil
}

// chosenVAtLayerForNVCheck returns the uniquely-retained V at layer (if any),
// without consulting host validity. Used by PhaseTwoEnd to identify the
// candidate V whose host-NV verdict triggers the CommitNV label.
func (i *Instance) chosenVAtLayerForNVCheck(layer int) (Value, bool) {
	leaderMap := i.bundles[layer]
	if len(leaderMap) == 0 {
		return nil, false
	}
	expectedLeader := i.cfg.Layers[layer].Leader
	retained := leaderMap[expectedLeader]
	if len(retained) != 1 {
		return nil, false
	}
	return retained[0].Value, true
}
