package base

import (
	"bytes"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
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
			tag := obft.NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
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
		tag := obft.NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
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
	// (per spec §Phase 2 / Wire format). Each witness ships value_root +
	// σ_V (~128 bytes); receivers cross-reference value_root against
	// retained V's. V-drop receivers (no V retained for this layer/leader)
	// recover via KindCertificate gossip per spec §Final-certificate gossip.
	var witnesses []LeaderSigmaWitness
	for layer, leaderMap := range i.bundles {
		for leader, retained := range leaderMap {
			for _, b := range retained {
				witnesses = append(witnesses, LeaderSigmaWitness{
					Layer:     layer,
					Leader:    leader,
					ValueRoot: ValueRoot(b.Value),
					SigmaV:    append(Signature{}, b.SigmaV...),
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
	// (single-emit rule, spec §Phase 2). All distinct hashes are retained
	// (up to MaxCommitHashesPerOp) so later redeliveries of any prior variant
	// remain no-ops; once the cap is reached, further distinct emissions are
	// silently dropped — the operator is already flagged repeatedly, accepting
	// more variants is just memory pressure.
	curHash := commitContentHash(c)
	seen := i.peerCommitHashes[c.OperatorID]
	if seen == nil {
		// First observation from this op: stash a deep copy so the second
		// distinct emission can package both bodies into Rule 3 evidence.
		i.peerCommitHashes[c.OperatorID] = map[[32]byte]struct{}{curHash: {}}
		i.peerFirstCommit[c.OperatorID] = deepCopyCommit(c)
	} else {
		if _, ok := seen[curHash]; ok {
			return nil // identical re-broadcast
		}
		if len(seen) >= MaxCommitHashesPerOp {
			// Operator is already flagged byzantine via prior distinct hashes;
			// drop this emission entirely (no per-content processing) to bound
			// memory under abuse.
			return nil
		}
		// Top-level Rule 3 fires once per (op, slot): the second distinct
		// emission carries the slashable payload. Subsequent distinct
		// emissions don't add attribution but their σ-side / NR-side content
		// still flows through the per-layer paths below.
		if first := i.peerFirstCommit[c.OperatorID]; first != nil {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossOnionEquivocation,
				OperatorID: c.OperatorID,
				Layer:      -1, // -1 = "spans the whole commit", not per-layer
				CommitEquivocation: &CommitEquivocationEvidence{
					CommitA: first,
					CommitB: deepCopyCommit(c),
				},
			})
			// Drop the retained first commit — Rule 3 has fired; any further
			// distinct emissions reuse per-layer Rule 1/3/5 evidence (which
			// is cheaper) and don't need the top-level pairing again.
			delete(i.peerFirstCommit, c.OperatorID)
		}
		seen[curHash] = struct{}{}
		// Continue processing so any new content (idempotent per-content
		// dedup below) is collected and per-layer Rule 1/3 checks fire on
		// new entries.
	}

	// Pre-validate NR partials BEFORE σ-side / NR-side / witness mutations,
	// for per-Commit atomicity on the inner-bundle state: a malformed NR
	// partial bails out without leaving σ-onion entries, peerNR entries,
	// or witness retentions behind.
	//
	// What survives a failed NR pre-validation: the top-level dedup
	// mutations above (peerCommitHashes, peerFirstCommit, top-level Rule 3
	// evidence). They must — the slashable proof (CommitEquivocation with
	// both Commit bodies) attributes the byzantine's structurally-distinct
	// emissions regardless of inner-bundle validity, and peerCommitHashes
	// is what makes the second-distinct-emission detection work. Per-layer
	// Rule 1/3/5 evidence is lost on bad-NR Commits but is redundant with
	// the top-level CommitEquivocationEvidence — slashing tools can derive
	// per-layer details from the full Commit bodies.
	//
	// In production the validation layer's Verifier.VerifyCommitNRPartials
	// rejects malformed NR before reaching this path; this is defense-in-
	// depth for any path that bypasses validation (tests, future plumbing).
	if err := i.verifyCommitNRPartials(c); err != nil {
		return err
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
		// one, this is cross-onion equivocation (Rule 3 per spec §Slashing
		// evidence) — the operator violated single-σ-V exclusivity.
		//
		// Action contract at k > 0: el.Ciphertext is the IBE-wrapped σ
		// partial (chained IBE encryption per spec §Phase 2). The evidence
		// is recorded for within-cluster attribution (the cluster has the
		// NR-quorum aggregates during Phase 3 reconstruction and can verify
		// the partials locally). It is NOT third-party self-contained:
		// on-chain slashing would need this cluster's NR-quorum aggregates
		// (transient Phase-3 state) plus an IBE chain-decrypting verifier
		// to reproduce the dual-V check.
		//
		// We deliberately don't pay that infra cost because the top-level
		// Rule 3 variant (Layer=-1, recorded elsewhere — pairs the FULL
		// Commits) is already self-contained at any layer and covers the
		// same byzantine fault. Per-layer at k > 0 is the cluster's
		// finer-grained record; on-chain slashing relies on Layer=-1.
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
				if bytes.Equal(b.Value, el.Value) {
					continue
				}
				if !i.recordRule3Leader(c.OperatorID, k) {
					break
				}
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

		// L_0 validity-gate: at L_0, σ partials are plaintext and verifiable
		// against the operator's pubkey share. Per spec §Slashing evidence
		// Rule 5, "a plaintext σ partial that does not verify against any
		// retained candidate V at that layer" is slashable. Two verdicts
		// trigger Rule 5:
		//
		//   - cryptoFake: partial fails verify on op's own pubshare for the
		//     claimed V. Unambiguous byzantine — fire immediately and drop
		//     the entry from peerOnions (it can't contribute to any V's
		//     σ-pool).
		//   - unknownV: partial verifies against op's pubshare on the
		//     claimed V, but V is not retained locally. Per spec literal
		//     reading, the partial "does not verify against any retained V"
		//     and Rule 5 fires. Under leader equivocation an honest peer
		//     who signed an equivocated V the receiver didn't get is a
		//     possible false-positive; spec MUST-log framing puts the
		//     false-positive resolution at the out-of-band aggregation
		//     layer (manual-blacklist mechanism). The entry stays in
		//     peerOnions — the partial is byte-valid and may still
		//     contribute to its V's σ-pool if V later becomes known via
		//     bundle retention or witness harvest. Skipped when the op is
		//     the layer's own leader: leader cross-V is Rule 3, not Rule 5,
		//     and the leader path is the most common source of legitimate
		//     unknownV (their own bundle vs their own onion equivocation).
		fakeAtL0 := false
		if k == 0 {
			switch i.peerSigmaAtL0Verdict(c.OperatorID, el) {
			case l0SigmaCryptoFake:
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
			case l0SigmaUnknownV:
				if c.OperatorID != i.cfg.Layers[0].Leader && i.recordRule5UnknownV(c.OperatorID, 0) {
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
			}
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
		// with here. Deduplicated per (op, layer) so multiple distinct
		// σ-emissions at the same layer (Rule 3) don't multi-record Rule 1.
		if i.peerNR[k] != nil {
			if nrSig, hasNR := i.peerNR[k][c.OperatorID]; hasNR && i.recordRule1(c.OperatorID, k) {
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

	// NR-side per layer. NR partial verification was already performed by
	// verifyCommitNRPartials before σ-side processing (per-Commit atomicity).
	// This loop is purely state mutation + Rule 1 cross-signing detection.
	for _, p := range c.NRPartials {
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
		// spec §Slashing-evidence (cross-phase exclusivity). Deduplicated
		// per (op, layer) — symmetry with the σ-side fire site.
		if onionEntries := i.peerOnions[p.Layer][c.OperatorID]; len(onionEntries) > 0 {
			if i.recordRule1(c.OperatorID, p.Layer) {
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
		} else if c.OperatorID == i.cfg.Layers[p.Layer].Leader {
			if bundles := i.bundles[p.Layer][c.OperatorID]; len(bundles) > 0 && i.recordRule1(c.OperatorID, p.Layer) {
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

	// Witnesses: per spec §Phase 2 wire format, witnesses ship value_root +
	// σ_V (no full V). The σ partial bytes are plaintext at every layer —
	// they're byte-for-byte copies of the leader's Phase-1 σ partial, not
	// subject to chained encryption. Receivers who have observed V locally
	// (via Phase-1 bundle retention or via any peer's σ-onion entry
	// carrying V) can cross-reference value_root, verify σ_V against the
	// leader's pubshare on V, and harvest into the σ-pool. Helps reach qV
	// when the leader's bundle never reached the receiver but V is known
	// via a different path. V-drop recovery (no V locally) still flows
	// through KindCertificate gossip per spec §Final-certificate gossip.
	for _, w := range c.Witnesses {
		i.harvestWitness(w)
	}

	return nil
}

// harvestWitness verifies a peer's witness against the leader's pubshare on
// any V the receiver has observed locally for the witness's layer, then
// retains the σ_V partial under (layer, value_root) for inclusion in the
// σ-pool at Resolve time. Idempotent (first valid harvest wins).
func (i *Instance) harvestWitness(w LeaderSigmaWitness) {
	if w.Layer < 0 || w.Layer >= i.cfg.K() {
		return
	}
	// Leader claim was verified structurally in ValidateCommit; defense in
	// depth.
	if w.Leader != i.cfg.Layers[w.Layer].Leader {
		return
	}
	bucket := i.witnessedLeaderSigma[w.Layer]
	if _, exists := bucket[w.ValueRoot]; exists {
		return
	}
	// Resolve V from local state: bundles retained at this layer for this
	// leader, OR any peerOnion entry at this layer carrying V plaintext.
	v, ok := i.findVByRoot(w.Layer, w.ValueRoot)
	if !ok {
		// V isn't known locally yet — drop the witness. v1 doesn't pend
		// for retroactive verification; see witnessedLeaderSigma comment
		// in instance.go for the cross-commit-out-of-order coverage gap.
		return
	}
	pubShare, ok := i.pubKeyShares[w.Leader]
	if !ok {
		return
	}
	if !i.signer.VerifyPartial(pubShare, v, w.SigmaV) {
		return
	}
	if bucket == nil {
		bucket = make(map[[32]byte]witnessedSigma)
		i.witnessedLeaderSigma[w.Layer] = bucket
	}
	bucket[w.ValueRoot] = witnessedSigma{
		Value:  append(Value{}, v...),
		SigmaV: append(Signature{}, w.SigmaV...),
	}
}

// findVByRoot returns the V bytes at `layer` whose sha256 matches `root`,
// looking in (1) retained Phase-1 bundles for the layer leader, (2) any
// peer's σ-onion entry at this layer (V is plaintext at every layer). Used
// to resolve a witness's value_root to V for σ_V verification.
func (i *Instance) findVByRoot(layer int, root [32]byte) (Value, bool) {
	for _, bundles := range i.bundles[layer] {
		for _, b := range bundles {
			if ValueRoot(b.Value) == root {
				return b.Value, true
			}
		}
	}
	for _, entries := range i.peerOnions[layer] {
		for _, el := range entries {
			if ValueRoot(el.Value) == root {
				return el.Value, true
			}
		}
	}
	return nil, false
}

// l0SigmaVerdict is the result of checking a peer's L_0 σ entry.
type l0SigmaVerdict int

const (
	l0SigmaInconclusive l0SigmaVerdict = iota // no retained V or no pub-share for op (config issue, not slashable)
	l0SigmaVerified                           // matches a retained V AND verifies cryptographically
	l0SigmaCryptoFake                         // partial does not verify on the claimed V (Rule 5 fires immediately)
	l0SigmaUnknownV                           // partial verifies but el.Value matches no currently-retained V (Rule 5 fires for non-leader emitters, dedup'd via recordRule5UnknownV; leader unknownV is Rule 3 territory)
)

// peerSigmaAtL0Verdict classifies a peer's plaintext L_0 σ partial.
//
// At L_0, el.Ciphertext is the plaintext σ partial signed by op's V-share over
// el.Value. The verdict drives Rule 5 evidence handling.
//
// Order of checks:
//  1. No pub-share for op → inconclusive (config issue, not slashable).
//  2. VerifyPartial(opPub, el.Value, el.Ciphertext) fails → cryptoFake. The op
//     signed bytes that don't verify against their own share for the V they
//     claimed; this is authenticated dishonesty regardless of whether any
//     L_0 bundle has been retained — fire Rule 5 immediately. Critically,
//     this check runs BEFORE the leaderMap-empty short-circuit so a byzantine
//     who emits unverifiable bytes while the L_0 leader is silent (no bundle
//     ever broadcast) still gets attributed.
//  3. el.Value matches a retained V → verified.
//  4. el.Value matches no retained V, but leaderMap is non-empty → unknownV.
//     The op signed a V we haven't retained yet; the leader MAY equivocate
//     later and broadcast that V, in which case reevaluation reclassifies
//     to verified. Currently NOT fired as Rule 5 anywhere — see
//     Instance.Finalize doc-comment for the false-positive rationale. Per
//     spec §Slashing evidence Rule 5, cluster-wide attribution for this
//     case is recovered via out-of-band log aggregation across operators.
//  5. el.Value matches no retained V AND leaderMap is empty → inconclusive.
//     Without any retention we cannot distinguish "fake V" from "leader
//     hasn't broadcast yet"; defer.
//
// Splitting the verdict (cryptoFake vs unknownV) avoids false-positive Rule 5
// against an honest peer who signed leader L's V_b before L's V_b reached
// our retention (L equivocated and we saw V_a first) — both the immediate
// fire path (ObserveCommit) and the retroactive fire path (reevaluateL0Sigmas
// on bundle retention) only act on cryptoFake, leaving unknownV as a "we
// can't tell from local view alone" deferral.
func (i *Instance) peerSigmaAtL0Verdict(op OperatorID, el EncryptedLayer) l0SigmaVerdict {
	pubShare, ok := i.pubKeyShares[op]
	if !ok {
		return l0SigmaInconclusive
	}
	if !i.signer.VerifyPartial(pubShare, el.Value, el.Ciphertext) {
		return l0SigmaCryptoFake
	}
	leaderMap := i.bundles[0]
	if len(leaderMap) == 0 {
		// Verify passed but no V retained yet — can't decide between
		// unknownV (V not retained yet but leader has retentions) and
		// verified (V matches retained). Defer.
		return l0SigmaInconclusive
	}
	for _, retained := range leaderMap {
		for _, b := range retained {
			if bytes.Equal(b.Value, el.Value) {
				return l0SigmaVerified
			}
		}
	}
	return l0SigmaUnknownV
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

// verifyCommitNRPartials runs the per-partial BLS verification used by
// ObserveCommit's pre-validation pass. Verification-only — no state mutation.
// Mirrors Verifier.VerifyCommitNRPartials but uses the Instance's own
// configured shares + signers, which is what gates whether a Commit is allowed
// to mutate Instance state.
//
// IBE shares fallback (Option A vs Option B): when ibePubKeyShares is nil,
// we fall back to pubKeyShares (the V-keypair shares). This is the Option A
// integration documented in docs/IBE-INTEGRATION.md — the validator's
// V-keypair shares double as IBE shares with cryptographic separation
// achieved via distinct domain-separation tags (DSTs) in the BLS primitive
// rather than a separate IBE keypair. Spec §Setting describes the threshold
// scheme as "two distinct keypairs" (V-keypair + IBE-keypair); Option A
// uses one keypair with DST-trick separation, which preserves the
// Pigeonhole 1 algebraic argument (same threshold qV = qEnc = 2f+1) while
// avoiding a second DKG. Option B (a separate IBE-keypair from per-cluster
// IBE-DKG) sets ibePubKeyShares to the IBE-derived shares; this code path
// then verifies against those instead.
func (i *Instance) verifyCommitNRPartials(c *Commit) error {
	if len(c.NRPartials) == 0 {
		return nil
	}
	nrShares := i.ibePubKeyShares
	if nrShares == nil {
		nrShares = i.pubKeyShares
	}
	pubShare, ok := nrShares[c.OperatorID]
	if !ok || len(pubShare) == 0 {
		return fmt.Errorf("obft: no NR pub-key share for operator %d", c.OperatorID)
	}
	for _, p := range c.NRPartials {
		tag := obft.NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, p.Layer)
		if !i.tagSigner.VerifyPartial(pubShare, tag, p.PartialSig) {
			return fmt.Errorf("obft: NR partial from op %d at layer %d failed verification",
				c.OperatorID, p.Layer)
		}
	}
	return nil
}
