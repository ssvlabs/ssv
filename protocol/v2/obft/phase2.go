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

	// Self-observe own contributions so the local Resolve pool counts them.
	// Without this, a non-leader operator's own σ partial never enters
	// peerOnions/peerNR, so the pool tops out at n−1 cluster-wide and only
	// the layer leader can locally reach qV. Symmetric to the Phase-1
	// self-observe in BuildPhase1Bundle.
	for k, el := range layers {
		if len(el.Value) == 0 {
			continue
		}
		// Own-leader layers: σ_V is already in i.bundles via Phase-1
		// self-observe; BuildOwnCommit emits no redundant onion at L_k
		// when leader, so this branch is skipped by the empty-Value check
		// above. (Belt-and-braces.)
		if i.cfg.Layers[k].Leader == i.ownOperatorID {
			continue
		}
		if i.peerOnions[k] == nil {
			i.peerOnions[k] = make(map[OperatorID][]EncryptedLayer)
		}
		if len(i.peerOnions[k][i.ownOperatorID]) == 0 {
			i.peerOnions[k][i.ownOperatorID] = []EncryptedLayer{{
				Value:      append(Value{}, el.Value...),
				Ciphertext: append([]byte{}, el.Ciphertext...),
			}}
		}
	}
	for _, p := range nrPartials {
		if i.peerNR[p.Layer] == nil {
			i.peerNR[p.Layer] = make(map[OperatorID]Signature)
		}
		if _, exists := i.peerNR[p.Layer][i.ownOperatorID]; !exists {
			i.peerNR[p.Layer][i.ownOperatorID] = append(Signature{}, p.PartialSig...)
		}
	}

	// Witnesses: include every Phase-1 bundle this operator has retained
	// (per spec §Phase 2 / Wire format / §Appendix C). Enables receivers who
	// missed the original Phase-1 broadcast to rehydrate the leader's σ
	// contribution from any peer's KindCommit.
	var witnesses []LeaderSigmaWitness
	for layer, leaderMap := range i.bundles {
		for leader, retained := range leaderMap {
			for _, b := range retained {
				witnesses = append(witnesses, LeaderSigmaWitness{
					Layer:  layer,
					Leader: leader,
					Value:  append(Value{}, b.Value...),
					SigmaV: append(Signature{}, b.SigmaV...),
				})
			}
		}
	}

	return &Commit{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layers:     layers,
		NRPartials: nrPartials,
		Witnesses:  witnesses,
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
//   - cross-checks σ + NR at the same layer from the same operator (Rule 1),
//     including the leader's own Phase-1 σ_V at their own layer;
//   - flags a structurally-distinct second KindCommit from the same operator
//     as cross-onion equivocation (top-level dedup via content hash).
func (i *Instance) ObserveCommit(c *Commit) error {
	if err := ValidateCommit(c, i.cfg); err != nil {
		return err
	}
	K := i.cfg.K()

	// Top-level dedup. Identical re-broadcasts (gossipsub) are no-ops; the
	// FIRST distinct second hash from the same operator is byzantine evidence
	// (single-emit rule, spec §Phase 2). All distinct hashes are retained so
	// later redeliveries of any prior variant remain no-ops.
	curHash := commitContentHash(c)
	seen := i.peerCommitHashes[c.OperatorID]
	if seen == nil {
		i.peerCommitHashes[c.OperatorID] = map[[32]byte]struct{}{curHash: {}}
	} else {
		if _, ok := seen[curHash]; ok {
			return nil // identical re-broadcast
		}
		i.recordEvidence(Evidence{
			Rule:       EvidenceCrossOnionEquivocation,
			OperatorID: c.OperatorID,
			Layer:      -1, // -1 = "spans the whole commit", not per-layer
		})
		seen[curHash] = struct{}{}
		// Continue processing so any new content (idempotent per-content
		// dedup below) is collected and per-layer Rule 1/3 checks fire on
		// new entries.
	}

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
		alreadyHaveValue := false
		for _, e := range existing {
			if bytes.Equal(e.Value, el.Value) {
				alreadyHaveValue = true
				break
			}
		}
		if alreadyHaveValue {
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

		// Cross-σ check vs the leader's Phase-1 σ_V at this layer (Rule 3
		// extension). A byzantine layer leader could σ-emit V_a via the
		// Phase-1 bundle and σ_V on V_b via this Phase-2 onion — single-σ-V
		// exclusivity violation that spans phases.
		//
		// Only fired at k=0: at deeper layers el.Ciphertext is the IBE-wrapped
		// σ partial, not a verifiable plaintext sig, so a third-party slashing
		// verifier cannot reproduce the check from the evidence alone (it
		// would need this cluster's NR-quorum aggregates for layers 0..k-1).
		// Self-contained slashable evidence (the spec's Rule 3 contract)
		// requires plaintext partials.
		if k == 0 && c.OperatorID == i.cfg.Layers[k].Leader {
			for _, b := range i.bundles[k][c.OperatorID] {
				if !bytes.Equal(b.Value, el.Value) {
					i.recordEvidence(Evidence{
						Rule:       EvidenceCrossOnionEquivocation,
						OperatorID: c.OperatorID,
						Layer:      k,
						CrossOnionEquivocation: &CrossOnionEquivocationEvidence{
							ValueA:   append(Value{}, b.Value...),
							ValueB:   append(Value{}, el.Value...),
							PartialA: append(Signature{}, b.SigmaV...),
							PartialB: append(Signature{}, el.Ciphertext...),
						},
					})
					break
				}
			}
		}

		// L_0 validity-gate: at L_0, σ partials are plaintext and verifiable
		// against any retained V. A peer's plaintext σ partial that does not
		// verify against any retained V at L_0 is a slashable byzantine fault
		// (Rule 5 — Fake plaintext σ at L_0). The fake partial doesn't enter
		// any V's σ-pool (no liveness impact); detection is for slashing.
		// We run the check BEFORE appending to peerOnions so a fake entry
		// never leaves traces in the σ-pool that a later code path could
		// accidentally consume.
		fakeAtL0 := false
		if k == 0 && i.peerSigmaAtL0Verdict(c.OperatorID, el) == l0SigmaFake {
			fakeAtL0 = true
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
		}

		if !fakeAtL0 {
			entryCopy := EncryptedLayer{
				Value:      append(Value{}, el.Value...),
				Ciphertext: append([]byte{}, el.Ciphertext...),
			}
			i.peerOnions[k][c.OperatorID] = append(existing, entryCopy)
		}

		// Cross-signing detection (Rule 1): σ + NR at the same (operator,
		// layer)? Fires whether the σ side is a Phase-2 onion (this entry)
		// or — for the layer's leader — a Phase-1 bundle σ_V already
		// recorded; Rule 1's NR side is what the σ-side onion path collides
		// with here.
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

	// NR-side per layer. Verify each partial against the signer's IBE pub-share
	// (under Option B) or V-pub-share (Option A: ibePubKeyShares == nil; the
	// V-keypair doubles as the IBE keypair via DST separation handled by
	// tagSigner). Without this, a single byzantine could submit garbage NR
	// partials that get aggregated into a corrupted chained-decryption key
	// at Phase 3, killing fall-through cluster-wide.
	nrShares := i.ibePubKeyShares
	if nrShares == nil {
		nrShares = i.pubKeyShares
	}
	for _, p := range c.NRPartials {
		pubShare, ok := nrShares[c.OperatorID]
		if !ok || len(pubShare) == 0 {
			return fmt.Errorf("obft: no NR pub-key share for operator %d", c.OperatorID)
		}
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, p.Layer)
		if !i.tagSigner.VerifyPartial(pubShare, tag, p.PartialSig) {
			return fmt.Errorf("obft: NR partial from op %d at layer %d failed verification",
				c.OperatorID, p.Layer)
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
		// a σ entry at this layer (in an onion or, for the layer's leader,
		// in the Phase-1 bundle)? Either case is a Rule 1 violation per
		// spec §Slashing-evidence (cross-phase exclusivity).
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
		} else if c.OperatorID == i.cfg.Layers[p.Layer].Leader {
			if bundles := i.bundles[p.Layer][c.OperatorID]; len(bundles) > 0 {
				i.recordEvidence(Evidence{
					Rule:       EvidenceCrossSigning,
					OperatorID: c.OperatorID,
					Layer:      p.Layer,
					CrossSigning: &CrossSigningEvidence{
						SigmaPartial: append(Signature{}, bundles[0].SigmaV...),
						SigmaValue:   append(Value{}, bundles[0].Value...),
						NRPartial:    append(Signature{}, p.PartialSig...),
					},
				})
			}
		}
	}

	// Witnesses: rehydrate leader-σ_V contributions from any retained
	// Phase-1 bundle the emitter packed. Treats each witness as if it were
	// a Phase-1 bundle observation; ObservePhase1Bundle handles σ-verification
	// and the 2-V retention bound (firing Rule 2 evidence on a distinct
	// second V at same (layer, leader)). observedOffset=0 bypasses the
	// Phase-1 acceptance window — witnesses are admissible past T_commit
	// since that's the whole point.
	for _, w := range c.Witnesses {
		bundle := &Phase1Bundle{
			ClusterID:  c.ClusterID,
			OperatorID: w.Leader,
			Height:     c.Height,
			Layer:      w.Layer,
			Value:      append(Value{}, w.Value...),
			SigmaV:     append(Signature{}, w.SigmaV...),
		}
		// Errors are best-effort: a malformed/unverifiable witness is
		// dropped silently rather than failing the whole Commit.
		_ = i.ObservePhase1Bundle(bundle, 0)
	}

	return nil
}

// l0SigmaVerdict is the result of checking a peer's L_0 σ entry.
type l0SigmaVerdict int

const (
	l0SigmaInconclusive l0SigmaVerdict = iota // no retained V or no pub-share for op (config issue, not slashable)
	l0SigmaVerified                           // matches a retained V AND verifies cryptographically
	l0SigmaFake                               // either signs a non-retained V or fails verification on a retained V (Rule 5)
)

// peerSigmaAtL0Verdict returns whether peer `op`'s plaintext σ partial at L_0
// is verified, fake (Rule 5), or inconclusive.
//
// At L_0, el.Ciphertext is the plaintext σ partial. We verify it against the
// operator's own pubkey share (the partial is signed by their V-share),
// using el.Value as the signed message.
//
// Cases:
//   - No retained V at L_0 → inconclusive (we can't decide without a V to
//     compare). Caller may decide to defer the check (re-run on retention).
//   - No pub-share for op → inconclusive (config error, not slashable).
//   - el.Value doesn't match any retained V → fake (Rule 5).
//   - el.Value matches but partial doesn't verify → fake (Rule 5).
//   - el.Value matches and partial verifies → verified.
func (i *Instance) peerSigmaAtL0Verdict(op OperatorID, el EncryptedLayer) l0SigmaVerdict {
	leaderMap := i.bundles[0]
	if len(leaderMap) == 0 {
		return l0SigmaInconclusive
	}
	pubShare, ok := i.pubKeyShares[op]
	if !ok {
		return l0SigmaInconclusive
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
		return l0SigmaFake // Rule 5: signed a V no honest retained
	}
	if !i.signer.VerifyPartial(pubShare, el.Value, el.Ciphertext) {
		return l0SigmaFake // Rule 5: signed bytes don't verify on the claimed V
	}
	return l0SigmaVerified
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
