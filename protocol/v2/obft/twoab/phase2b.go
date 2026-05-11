package twoab

import (
	"bytes"
	"fmt"
)

// BuildOwnOnion2b constructs the local operator's Phase-2b emission for
// this slot — a single Onion2b carrying per-layer σ or NR commitments
// per the convergence rule's decision for each layer.
//
// Per spec §Phase 2b emission:
//
//   - Each operator emits exactly one Onion2b per (slot, operator);
//     subsequent calls return the cached emission (idempotent).
//   - Per-layer σ partials are plaintext at L_0 and chained-IBE-encrypted
//     at deeper layers (k > 0) under nr_tag_0...nr_tag_{k-1}.
//   - Per-layer NR partials are σ_i^{IBE}(nr_tag_k) for k ∈ [0, K-1).
//     No NR partial at the deepest layer K-1 (no nr_tag exists).
//   - EKM enforces single-σ-V per (slot, layer) and σ-XOR-NR per layer
//     at sign time. A bug that requested σ-then-NR at the same layer
//     (or σ on two different V's) is caught by transitionToSigma /
//     transitionToNR before any wire bytes leave this method.
//
// The constructed Onion2b is NOT self-observed — the caller is expected
// to broadcast it and explicitly call ObserveOnion2b at every receiver
// (including the local operator). This mirrors Phase-E's
// BuildPhase1Bundle / Phase-F's BuildVerdict patterns.
func (i *Instance) BuildOwnOnion2b() (*Onion2b, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ownOnion2b != nil {
		return i.ownOnion2b, nil // idempotent re-build
	}
	K := i.cfg.K()
	layers := make([]EncryptedLayer, K)
	var nrPartials []NRPartial

	for k := 0; k < K; k++ {
		choice, value, err := i.ConvergenceDecision(k)
		if err != nil {
			return nil, fmt.Errorf("twoab: convergence at layer %d: %w", k, err)
		}
		switch choice {
		case CommitChoiceSigma:
			// EKM σ-lock first; only sign if EKM accepts.
			if err := i.transitionToSigma(k, value); err != nil {
				return nil, fmt.Errorf("twoab: σ-emit at layer %d: %w", k, err)
			}
			// Cache σ partial; deterministic signing means re-builds are
			// byte-identical.
			partial, cached := i.ownPartials[k]
			if !cached {
				p, err := i.signer.SignPartial(value)
				if err != nil {
					return nil, fmt.Errorf("twoab: sign σ at layer %d: %w", k, err)
				}
				i.ownPartials[k] = p
				partial = p
			}
			ct, err := i.chainEncryptForLayer(k, partial)
			if err != nil {
				return nil, fmt.Errorf("twoab: encrypt at layer %d: %w", k, err)
			}
			layers[k] = EncryptedLayer{
				Value:      append(Value{}, value...),
				Ciphertext: ct,
			}

		case CommitChoiceNR:
			// EKM NR-lock first.
			if err := i.transitionToNR(k); err != nil {
				return nil, fmt.Errorf("twoab: NR-emit at layer %d: %w", k, err)
			}
			// Deepest layer has no NR tag — produces no on-wire emission
			// per spec §Convergence rule "Deepest-layer NR has no on-wire
			// emission". The EKM lock above still binds the operator to
			// NR at K-1 (no σ-emission later), but no wire bytes are
			// produced.
			if k >= K-1 {
				continue
			}
			tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
			sig, err := i.tagSigner.SignPartial(tag)
			if err != nil {
				return nil, fmt.Errorf("twoab: sign NR partial at layer %d: %w", k, err)
			}
			nrPartials = append(nrPartials, NRPartial{
				Layer:      k,
				PartialSig: sig,
			})
		}
	}

	out := &Onion2b{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layers:     layers,
		NRPartials: nrPartials,
	}
	i.ownOnion2b = out
	return out, nil
}

// OwnOnion2b returns the local operator's cached Onion2b emission, or
// (nil, false) if BuildOwnOnion2b hasn't been called yet.
func (i *Instance) OwnOnion2b() (*Onion2b, bool) {
	if i.ownOnion2b == nil {
		return nil, false
	}
	return i.ownOnion2b, true
}

// ObserveOnion2b records a peer's (or the local operator's own, after
// broadcast) Phase-2b Onion2b. Per spec §Phase 2b:
//
//   - Each operator emits exactly one Onion2b per (slot, operator); a
//     second structurally-distinct emission is Rule 3 (cross-onion
//     equivocation) at Layer=-1.
//   - Per-layer σ-side and NR-side commitments are checked for cross-
//     phase exclusivity (Rule 1 σ + NR same layer same op).
//   - Per-layer σ entries on distinct V's at the same (op, layer)
//     trigger Rule 3 per-layer evidence.
//   - L_0 σ partials are plaintext-verifiable against the op's pub-share;
//     a partial that doesn't verify (cryptoFake) or that verifies but
//     on a V not retained locally (unknownV) triggers Rule 5.
//   - Phase-2b actions are cross-referenced against the op's broadcast
//     verdict at the same layer (Rule 6b verdict-vs-action).
func (i *Instance) ObserveOnion2b(o *Onion2b) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidateOnion2b(o, i.cfg); err != nil {
		return err
	}
	K := i.cfg.K()

	// Top-level dedup + Rule-3-at-Layer=-1 detection.
	curHash := onion2bContentHash(o)
	seen := i.peerOnion2bHashes[o.OperatorID]
	if seen == nil {
		i.peerOnion2bHashes[o.OperatorID] = map[[32]byte]struct{}{curHash: {}}
		i.peerFirstOnion2b[o.OperatorID] = deepCopyOnion2b(o)
	} else {
		if _, ok := seen[curHash]; ok {
			return nil // identical re-broadcast — silent dedup
		}
		// First distinct second emission → top-level Rule 3 evidence.
		if first := i.peerFirstOnion2b[o.OperatorID]; first != nil {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossOnionEquivocation,
				OperatorID: o.OperatorID,
				Layer:      -1,
				OnionEquivocation: &OnionEquivocationEvidence{
					OnionA: first,
					OnionB: deepCopyOnion2b(o),
				},
			})
			// Drop the retained first — Rule-3-at-Layer=-1 has fired.
			delete(i.peerFirstOnion2b, o.OperatorID)
		}
		seen[curHash] = struct{}{}
		// Continue processing — per-layer Rule 1/3/5 checks fire on the
		// new content.
	}

	// σ-side per layer.
	for k := 0; k < K; k++ {
		el := o.Layers[k]
		if len(el.Value) == 0 || len(el.Ciphertext) == 0 {
			continue // operator did not σ-emit at this layer
		}

		if i.peerOnions[k] == nil {
			i.peerOnions[k] = make(map[OperatorID][]EncryptedLayer)
		}
		existing := i.peerOnions[k][o.OperatorID]

		// Skip if same V already retained from this op at this layer.
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

		// Distinct V at the same (layer, op). If we already had one,
		// this is Rule 3 cross-onion equivocation per-layer evidence.
		if len(existing) == 1 {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossOnionEquivocation,
				OperatorID: o.OperatorID,
				Layer:      k,
				CrossOnionEquivocation: &CrossOnionEquivocationEvidence{
					ValueA:   existing[0].Value,
					ValueB:   el.Value,
					PartialA: existing[0].Ciphertext,
					PartialB: el.Ciphertext,
				},
			})
		}
		// Cap retention at 2 distinct V entries per (op, layer).
		if len(existing) >= 2 {
			continue
		}

		// L_0 σ-validity gate per spec §Slashing evidence Rule 5: a
		// plaintext σ partial that does not verify against any retained
		// candidate V is slashable. Two verdicts trigger Rule 5:
		//
		//   - cryptoFake: partial fails verify against op's pubshare on
		//     the claimed V. Unambiguous byzantine → fire immediately;
		//     drop the entry (it can't contribute to any V's σ-pool).
		//   - unknownV: partial verifies against op's pubshare on the
		//     claimed V but V is not retained locally. Per spec, fire
		//     Rule 5 (false-positive risk under leader equivocation —
		//     resolved out-of-band by aggregation per spec MUST-log
		//     framing). Skip when the op is the layer's own leader
		//     (leader cross-V is the Rule-3 leader extension, not
		//     Rule 5).
		//
		// Both branches dedup via recordRule5Fired so re-emissions under
		// top-level cross-onion equivocation don't accumulate duplicate
		// Rule 5 entries in the Evidence() slice.
		fakeAtL0 := false
		if k == 0 {
			switch i.peerSigmaAtL0Verdict(o.OperatorID, el) {
			case l0SigmaCryptoFake:
				fakeAtL0 = true
				if i.recordRule5Fired(o.OperatorID, 0) {
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
				}
			case l0SigmaUnknownV:
				if o.OperatorID != i.cfg.Layers[0].Leader && i.recordRule5Fired(o.OperatorID, 0) {
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
				}
			}
		}

		if !fakeAtL0 {
			entryCopy := EncryptedLayer{
				Value:      append(Value{}, el.Value...),
				Ciphertext: append([]byte{}, el.Ciphertext...),
			}
			i.peerOnions[k][o.OperatorID] = append(existing, entryCopy)
		}

		// Rule 1 (cross-signing): σ + NR at the same (op, layer) within
		// the same Onion2b. Check whether op also has an NR partial at
		// this layer.
		if i.peerNR[k] != nil {
			if nrSig, hasNR := i.peerNR[k][o.OperatorID]; hasNR && i.recordRule1(o.OperatorID, k) {
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

	// NR-side per partial.
	for _, p := range o.NRPartials {
		if i.peerNR[p.Layer] == nil {
			i.peerNR[p.Layer] = make(map[OperatorID]Signature)
		}
		if _, exists := i.peerNR[p.Layer][o.OperatorID]; exists {
			// Already have an NR partial for (op, layer); ignore the
			// second (the top-level dedup already records cross-onion
			// equivocation at Layer=-1; per-layer doesn't add slashable
			// info for NR).
			continue
		}
		i.peerNR[p.Layer][o.OperatorID] = append(Signature{}, p.PartialSig...)

		// Rule 1 (cross-signing): NR after a σ entry at the same (op,
		// layer). Symmetric to the σ-side check above.
		if i.peerOnions[p.Layer] != nil && len(i.peerOnions[p.Layer][o.OperatorID]) > 0 {
			if i.recordRule1(o.OperatorID, p.Layer) {
				sigmaEntry := i.peerOnions[p.Layer][o.OperatorID][0]
				i.recordEvidence(Evidence{
					Rule:       EvidenceCrossSigning,
					OperatorID: o.OperatorID,
					Layer:      p.Layer,
					CrossSigning: &CrossSigningEvidence{
						SigmaPartial: append(Signature{}, sigmaEntry.Ciphertext...),
						SigmaValue:   append(Value{}, sigmaEntry.Value...),
						NRPartial:    append(Signature{}, p.PartialSig...),
					},
				})
			}
		}
	}

	// Rule 6b (verdict-vs-action): cross-reference the op's broadcast
	// verdict at each layer against their Phase-2b action.
	i.checkRule6b(o)

	return nil
}

// checkRule6b cross-references the op's broadcast verdict at each layer
// against their Phase-2b action observed in this Onion2b. Per spec
// §Slashing evidence Rule 6b, the following pairs are contradictions:
//
//   - σV verdict + NR action: op declared σ-eligibility on V at Phase 2a
//     but emitted NR at Phase 2b. Could be honest revision (equivocation
//     observed mid-Phase-2a) — log the evidence; out-of-band
//     aggregation distinguishes.
//   - NR/NV verdict + σ action: op declared no-σ-side at Phase 2a but
//     emitted σ at Phase 2b. Less common but still a verdict-vs-action
//     mismatch.
//
// Honest revision is permitted at the protocol layer — Rule 6b is
// boundary-conditional; receivers log and aggregate out-of-band. The
// (op, layer) dedup ensures one evidence entry per logical fault per
// receiver.
//
// Verdict equivocators are skipped: Rule 6a has already fired against
// them, and the "first-observed verdict" retained in peerVerdicts isn't
// authoritative for the byzantine sender (they broadcast a contradictory
// second verdict). Flagging Rule 6b against the first verdict would be
// spurious — the byzantine could have broadcast {σV, NR} and emitted
// either; whichever they emit "contradicts" the other verdict.
func (i *Instance) checkRule6b(o *Onion2b) {
	for k := range o.Layers {
		if i.IsEquivocator(k, o.OperatorID) {
			continue // Rule 6a subsumes; Rule 6b against first-observed verdict would be spurious
		}
		verdict, hasVerdict := i.peerVerdicts[k][o.OperatorID]
		if !hasVerdict {
			continue // can't check — no verdict observed
		}
		// What did the op actually emit at layer k?
		el := o.Layers[k]
		emittedSigma := len(el.Value) > 0 && len(el.Ciphertext) > 0
		// NR partial check.
		emittedNR := false
		if nrMap := i.peerNR[k]; nrMap != nil {
			_, emittedNR = nrMap[o.OperatorID]
		}

		switch verdict.Kind {
		case VerdictSigmaV:
			// σV verdict + NR action → mismatch
			if emittedNR && !emittedSigma && i.recordRule6b(o.OperatorID, k) {
				i.recordVerdictActionEvidence(o, k, verdict, false /*sigma*/)
			}
		case VerdictNR, VerdictNV:
			// NR/NV verdict + σ action → mismatch
			if emittedSigma && !emittedNR && i.recordRule6b(o.OperatorID, k) {
				i.recordVerdictActionEvidence(o, k, verdict, true /*sigma*/)
			}
		}
	}
}

func (i *Instance) recordVerdictActionEvidence(o *Onion2b, layer int, verdict *Verdict, actionSigma bool) {
	ev := Evidence{
		Rule:       EvidenceVerdictAction,
		OperatorID: o.OperatorID,
		Layer:      layer,
		VerdictAction: &VerdictActionEvidence{
			Verdict: deepCopyVerdict(verdict),
		},
	}
	if actionSigma {
		el := o.Layers[layer]
		ev.VerdictAction.SigmaValue = append(Value{}, el.Value...)
		ev.VerdictAction.SigmaPartial = append(Signature{}, el.Ciphertext...)
	} else {
		if nrSig, ok := i.peerNR[layer][o.OperatorID]; ok {
			ev.VerdictAction.NRPartial = append(Signature{}, nrSig...)
		}
	}
	i.recordEvidence(ev)
}

// l0SigmaVerdict is the result of checking a peer's L_0 σ entry.
type l0SigmaVerdict int

const (
	l0SigmaInconclusive l0SigmaVerdict = iota // no retained V or no pub-share (config issue)
	l0SigmaVerified                           // matches a retained V AND verifies cryptographically
	l0SigmaCryptoFake                         // partial does not verify on the claimed V (Rule 5 fires immediately)
	l0SigmaUnknownV                           // partial verifies on op's claimed V but V is not retained
)

// peerSigmaAtL0Verdict classifies a peer's plaintext L_0 σ partial.
//
// Order of checks:
//  1. No pub-share for op → inconclusive (config issue, not slashable).
//  2. VerifyPartial(opPub, el.Value, el.Ciphertext) fails → cryptoFake.
//     The op signed bytes that don't verify against their own share for
//     the V they claimed — authenticated dishonesty regardless of
//     whether any L_0 bundle has been retained. Fire Rule 5 immediately.
//  3. el.Value matches a retained V (from the layer's leader at L_0)
//     → verified.
//  4. el.Value matches no retained V, but some retention exists at L_0
//     → unknownV. Op signed a V we haven't retained; the leader MAY
//     equivocate later and broadcast that V, in which case the entry
//     could be reclassified — but the protocol doesn't yet implement
//     retroactive re-evaluation.
//  5. el.Value matches no retained V AND no L_0 retention exists at all
//     → inconclusive (no way to distinguish "fake V" from "leader hasn't
//     broadcast yet").
func (i *Instance) peerSigmaAtL0Verdict(op OperatorID, el EncryptedLayer) l0SigmaVerdict {
	opPub, ok := i.pubKeyShares[op]
	if !ok || len(opPub) == 0 {
		return l0SigmaInconclusive
	}
	if !i.signer.VerifyPartial(opPub, el.Value, el.Ciphertext) {
		return l0SigmaCryptoFake
	}
	hasAnyRetention := false
	for _, retained := range i.retainedBundles[0] {
		for _, r := range retained {
			if bytes.Equal(r.Bundle.Value, el.Value) {
				return l0SigmaVerified
			}
		}
		if len(retained) > 0 {
			hasAnyRetention = true
		}
	}
	if hasAnyRetention {
		return l0SigmaUnknownV
	}
	return l0SigmaInconclusive
}

// retainedL0ValueHashes returns sha256 hashes of all V's retained at
// L_0 (across all leaders, though in practice only one leader per
// layer). Used to populate Rule 5 evidence with the receiver's
// reference set for third-party verification.
func (i *Instance) retainedL0ValueHashes() [][]byte {
	leaderMap := i.retainedBundles[0]
	if leaderMap == nil {
		return nil
	}
	var out [][]byte
	for _, retained := range leaderMap {
		for _, r := range retained {
			h := ValueRoot(r.Bundle.Value)
			out = append(out, h[:])
		}
	}
	return out
}

// deepCopyOnion2b returns an independent copy of o so retention state
// isn't affected by caller-owned slice mutation post-Observe.
func deepCopyOnion2b(o *Onion2b) *Onion2b {
	if o == nil {
		return nil
	}
	layers := make([]EncryptedLayer, len(o.Layers))
	for k, el := range o.Layers {
		layers[k] = EncryptedLayer{
			Value:      append(Value{}, el.Value...),
			Ciphertext: append([]byte{}, el.Ciphertext...),
		}
	}
	nrPartials := make([]NRPartial, len(o.NRPartials))
	for j, p := range o.NRPartials {
		nrPartials[j] = NRPartial{
			Layer:      p.Layer,
			PartialSig: append(Signature{}, p.PartialSig...),
		}
	}
	return &Onion2b{
		ClusterID:  o.ClusterID,
		OperatorID: o.OperatorID,
		Height:     o.Height,
		Layers:     layers,
		NRPartials: nrPartials,
	}
}
