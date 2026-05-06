package obft

import (
	"bytes"
	"fmt"
)

// BuildOwnCommit builds the local operator's KindCommit message at T_commit.
// Per spec §Phase 2, each operator emits exactly one KindCommit per (slot,
// operator), based on what they observed by T_commit. The message bundles:
//
//   - σ partials for layers where the operator is σ-state (uniquely retained
//     host-validated V, no equivocation observed at this layer, no prior
//     NR-lock). σ partials at L_0 are plaintext; deeper layers are wrapped
//     in chained encryption gated on prior layers' NR-quorum.
//   - NR partials for layers in [0, K-1) where the operator is NR-state
//     (no V retained at T_commit, ≥ 2 V's retained = equivocation, or host
//     returned not-valid).
//
// EKM enforcement: σ-emission locks sigmaLocked[layer] on the chosen V;
// NR-emission locks nrLocked[layer]. Returns ErrAlreadyCommitted if called
// more than once.
func (i *Instance) BuildOwnCommit() (*Commit, error) {
	if i.committed {
		return nil, ErrAlreadyCommitted
	}
	K := i.cfg.K()
	layers := make([]EncryptedLayer, K)
	var nrPartials []NRPartial

	for k := 0; k < K; k++ {
		// At layers where the local op is the designated leader: if they
		// already σ-locked via BuildPhase1Bundle (their Phase-1 σ_V is the
		// σ-side commitment, picked up by Resolve from i.bundles), skip
		// emitting a redundant onion entry at this layer. If they did NOT
		// broadcast (silent leader), fall through to NR — a silent leader
		// must contribute their NR partial at their own layer or the NR
		// pool falls short.
		if i.cfg.Layers[k].Leader == i.ownOperatorID {
			if i.sigmaLocked[k] {
				continue
			}
			// Silent leader at own layer → NR path below.
			if k >= K-1 {
				continue // deepest layer has no NR tag
			}
			if err := i.transitionToNR(k, CommitNRSilent); err != nil {
				continue
			}
			tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
			sig, err := i.tagSigner.SignPartial(tag)
			if err != nil {
				return nil, fmt.Errorf("obft: sign NR partial at own-leader layer %d: %w", k, err)
			}
			nrPartials = append(nrPartials, NRPartial{
				Layer:      k,
				PartialSig: sig,
			})
			continue
		}

		// σ path: uniquely retained V, host validates, not NR-locked.
		if v, ok := i.chosenVForLayer(k); ok {
			if err := i.transitionToSigma(k, v); err == nil {
				partial, cached := i.ownPartials[k]
				if !cached {
					sig, err := i.signer.SignPartial(v)
					if err != nil {
						return nil, fmt.Errorf("obft: sign σ at layer %d: %w", k, err)
					}
					i.ownPartials[k] = sig
					partial = sig
				}
				ct, err := i.chainEncryptForLayer(k, partial)
				if err != nil {
					return nil, fmt.Errorf("obft: encrypt layer %d: %w", k, err)
				}
				layers[k] = EncryptedLayer{
					Value:      append(Value{}, v...),
					Ciphertext: ct,
				}
				continue
			}
			// transitionToSigma failed (already NR-locked). Fall through to NR.
		}

		// NR path: layers in [0, K-1) emit an IBE partial on nr_tag_k. The
		// deepest layer (K-1) has no NR tag — leave it as no contribution.
		if k >= K-1 {
			continue
		}
		// Determine NV vs NR-silent for local diagnostic. NV requires the
		// operator to have a uniquely retained V whose host verdict was
		// not-valid; otherwise it's NR-silent (no V retained, equivocation,
		// or host hasn't been asked).
		target := CommitNRSilent
		if v, retained := i.chosenVAtLayer(k); retained {
			if verdicts := i.hostVerdict[k]; verdicts != nil {
				if valid, recorded := verdicts[valueRootKey(v)]; recorded && !valid {
					target = CommitNV
				}
			}
		}
		if err := i.transitionToNR(k, target); err != nil {
			// σ-locked already (shouldn't happen given the σ branch above
			// would have continued); skip rather than corrupt EKM log.
			continue
		}
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
		sig, err := i.tagSigner.SignPartial(tag)
		if err != nil {
			return nil, fmt.Errorf("obft: sign NR partial at layer %d: %w", k, err)
		}
		nrPartials = append(nrPartials, NRPartial{
			Layer:      k,
			PartialSig: sig,
		})
	}

	i.committed = true
	return &Commit{
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layers:     layers,
		NRPartials: nrPartials,
	}, nil
}

// ObserveCommit records a peer's KindCommit message. Per spec §Phase 2, each
// honest operator emits exactly one KindCommit per (slot, operator); a second
// distinct KindCommit from the same operator is cross-onion equivocation
// (Rule 3) — the operator double-committed.
//
// This method:
//   - extracts σ entries (one per σ-state layer) into peerOnions, applying the
//     L_0 fake-σ check (Rule 5) when a retained V exists at L_0;
//   - extracts NR partials (one per NR-state layer) into peerNR;
//   - cross-checks σ + NR at the same layer from the same operator (Rule 1).
//
// On a second KindCommit from the same operator: the layers are checked against
// any already-recorded entries; distinct entries record cross-onion
// equivocation evidence.
func (i *Instance) ObserveCommit(c *Commit) error {
	if err := ValidateCommit(c, i.cfg); err != nil {
		return err
	}
	K := i.cfg.K()

	// σ-side per layer.
	for k := 0; k < K; k++ {
		el := c.Layers[k]
		if len(el.Value) == 0 || len(el.Ciphertext) == 0 {
			continue // operator did not σ-emit at this layer
		}

		if i.peerOnions[k] == nil {
			i.peerOnions[k] = make(map[OperatorID][]EncryptedLayer)
		}
		existing := i.peerOnions[k][c.OperatorID]

		// Find existing entry with the same value.
		seen := false
		for _, e := range existing {
			if bytes.Equal(e.Value, el.Value) {
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
				continue
			}
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossOnionEquivocation,
				OperatorID: c.OperatorID,
				Layer:      k,
				CrossOnionEquivocation: &CrossOnionEquivocationEvidence{
					ValueA:   existing[0].Value,
					ValueB:   el.Value,
					PartialA: existing[0].Ciphertext,
					PartialB: el.Ciphertext,
				},
			})
		}

		entryCopy := EncryptedLayer{
			Value:      append(Value{}, el.Value...),
			Ciphertext: append([]byte{}, el.Ciphertext...),
		}
		i.peerOnions[k][c.OperatorID] = append(existing, entryCopy)

		// L_0 validity-gate: at L_0, σ partials are plaintext and verifiable
		// against any retained V. A peer's plaintext σ partial that does not
		// verify against any retained V at L_0 is a slashable byzantine fault
		// (Rule 5 — Fake plaintext σ at L_0). The fake partial does not enter
		// any V's σ-pool (it doesn't verify), so it has no liveness impact;
		// detection is purely for slashing accountability.
		if k == 0 && i.hasRetainedVAtL0() {
			if !i.peerSigmaAtL0Verifies(c.OperatorID, el) {
				i.recordEvidence(Evidence{
					Rule:       EvidenceFakePlaintextSigma,
					OperatorID: c.OperatorID,
					Layer:      0,
					FakePlaintextSigma: &FakePlaintextSigmaEvidence{
						OnionPartial:        append(Signature{}, el.Ciphertext...),
						OnionValue:          append(Value{}, el.Value...),
						RetainedValueHashes: i.retainedL0ValueHashes(),
					},
				})
				i.removeOnionEntry(k, c.OperatorID, &el)
			}
		}

		// Cross-signing detection (Rule 1): σ + NR at the same (operator,
		// layer)? An honest operator commits exclusively per layer within a
		// single KindCommit; this would only fire for a malformed/byzantine
		// commit that emits both at the same layer.
		if i.peerNR[k] != nil {
			if nrSig, hasNR := i.peerNR[k][c.OperatorID]; hasNR {
				i.recordEvidence(Evidence{
					Rule:       EvidenceCrossSigning,
					OperatorID: c.OperatorID,
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

	// NR-side per layer.
	for _, p := range c.NRPartials {
		if i.ibePubKeyShares != nil {
			pubShare, ok := i.ibePubKeyShares[c.OperatorID]
			if !ok {
				return fmt.Errorf("obft: no IBE pubkey share for operator %d", c.OperatorID)
			}
			tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, p.Layer)
			if !i.tagSigner.VerifyPartial(pubShare, tag, p.PartialSig) {
				return fmt.Errorf("obft: NR partial from op %d at layer %d failed verification",
					c.OperatorID, p.Layer)
			}
		}
		if i.peerNR[p.Layer] == nil {
			i.peerNR[p.Layer] = make(map[OperatorID]Signature)
		}
		// Idempotent on duplicate observation.
		if _, exists := i.peerNR[p.Layer][c.OperatorID]; exists {
			continue
		}
		i.peerNR[p.Layer][c.OperatorID] = append(Signature{}, p.PartialSig...)

		// Rule 1 — cross-signing detection: did this operator already have
		// a σ entry at this layer in this same commit (or an earlier one)?
		if onionEntries := i.peerOnions[p.Layer][c.OperatorID]; len(onionEntries) > 0 {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossSigning,
				OperatorID: c.OperatorID,
				Layer:      p.Layer,
				CrossSigning: &CrossSigningEvidence{
					SigmaPartial: append(Signature{}, onionEntries[0].Ciphertext...),
					SigmaValue:   append(Value{}, onionEntries[0].Value...),
					NRPartial:    append(Signature{}, p.PartialSig...),
				},
			})
		}
	}

	i.peerCommitted[c.OperatorID] = true
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

// chosenVAtLayer returns the uniquely-retained V at layer (if any), without
// consulting host validity. Used by BuildOwnCommit to identify the candidate
// V whose host-NV verdict triggers the CommitNV label vs CommitNRSilent.
func (i *Instance) chosenVAtLayer(layer int) (Value, bool) {
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
