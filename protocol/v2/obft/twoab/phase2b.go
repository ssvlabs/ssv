package twoab

import (
	"bytes"
	"fmt"
)

// Phase 2b dynamic commit emission. Per spec §Phase 2b:
//
// Each operator emits at most one KindCommit per slot. Emission is
// trigger-driven (NO T_commit hard wall) — the Instance is a passive
// state machine that evaluates three triggers in priority order on
// every state delta:
//
//   1. Equivocation trigger (highest priority): op retained ≥ 2 distinct
//      V_0 from the L_0 leader → emit KindCommit-NR (A4 pivot if op had
//      previously emitted KindValue; otherwise KindCommit-NRDirect at
//      Phase 2a — handled in MaybeFirePhase2a).
//   2. σ-eligibility trigger: cluster's value_pool[V_0] reaches qV →
//      side decision per op-local state:
//        - V_local = V_0 AND host re-validates valid → KindCommit-Signed
//          (A2 from KindValue, or A6 from KindNoValue → KindValue → ...).
//        - Else (no V_local, V_local ≠ V_0, or host re-validates NV) →
//          KindCommit-NR (A3 host-flip pivot, A5 V-drop NR commit).
//   3. NR-eligibility trigger (lowest priority): cluster's noValuePool[L_0]
//      reaches qEnc AND op cannot σ at L_0 (cannot-σ gate) → KindCommit-NR.
//      The gate is structurally necessary to prevent σ-eligible ops from
//      forfeiting σ contributions to premature NR-side emission (see spec
//      §Trigger rules / "Why NR-eligibility has the cannot-σ gate").
//
// Per spec §Emission ordering, the upgrade check (Phase 2a-late A1)
// runs BEFORE commit trigger evaluation on every state delta.

// afterStateDelta is the per-tick processing cascade invoked at the end
// of every Observe* method (and ApplyHostValidity). Implements the
// normative emission ordering from spec §Emission ordering:
//
//  1. Run MaybeBuildAndBroadcastUpgrade — A1 upgrade if preconditions
//     are now satisfied.
//  2. Run MaybeBuildAndBroadcastCommit — three-trigger evaluation.
//
// Upgrade-first ensures the A1 sequence is preserved (A6: KindNoValue →
// KindValue → KindCommit-Signed) when a NoValueMsg-path op observes
// cluster σ-eligibility while simultaneously receiving V_0 via reflood.
// Each helper is independently idempotent: returns early when conditions
// aren't met or when the relevant emission has already fired.
//
// afterStateDelta is idempotent and safe to call repeatedly. After the
// op has emitted its single Commit, subsequent calls are no-ops.
func (i *Instance) afterStateDelta() {
	_, _ = i.MaybeBuildAndBroadcastUpgrade()
	_, _ = i.MaybeBuildAndBroadcastCommit()
}

// MaybeBuildAndBroadcastCommit evaluates the three Phase-2b commit
// triggers in priority order (equivocation > σ-eligibility >
// NR-eligibility) and, if any fires AND the op has not yet emitted a
// KindCommit, builds + records the appropriate Commit. Idempotent.
//
// Preconditions for emission:
//   - Op has emitted a Phase-2a coordination message (ownValueMsg or
//     ownNoValueMsg). NRDirect path (ownCommit set at Phase 2a) doesn't
//     get a second Commit per spec §Phase 2b ("Phase 2a NRDirect emitters
//     do NOT emit a second Commit").
//   - Op has not already emitted KindCommit (ownCommit nil).
//
// Returns:
//   - (*Commit, nil) on successful emission. Caller broadcasts.
//   - (nil, nil) if no trigger fires (silent wait — the slot's relay-
//     submission deadline at the runner level is the only hard wall).
//   - (nil, err) on internal build failure.
func (i *Instance) MaybeBuildAndBroadcastCommit() (*Commit, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ownCommit != nil {
		return nil, nil // already emitted
	}
	if i.ownValueMsg == nil && i.ownNoValueMsg == nil {
		// Phase 2a hasn't fired yet (or fired NRDirect, in which case
		// ownCommit is set and the above check catches it).
		return nil, nil
	}

	const layer = 0

	// Trigger 1 (highest priority): equivocation.
	if i.equivocationObservedAtLayer(layer) {
		return i.buildCommitNR()
	}

	// Trigger 2: σ-eligibility (fires-on-pool, side-on-state).
	if vRoot, count := i.valuePoolMaxV(layer); count >= i.cfg.QV() {
		return i.buildCommitForSigmaEligibility(vRoot)
	}

	// Trigger 3: NR-eligibility (with cannot-σ gate).
	if i.noValuePoolSize(layer) >= i.cfg.QEnc() && !i.canSigmaAtLayer(layer) {
		return i.buildCommitNR()
	}

	// No trigger fired.
	return nil, nil
}

// buildCommitForSigmaEligibility builds the appropriate Commit when the
// σ-eligibility trigger fires on `eligibleVRoot`. Side decision per
// op-local state:
//   - V_local = eligibleVRoot AND host re-validates valid → KindCommit-Signed (A2/A6).
//   - Else → KindCommit-NR (A3 host-flip pivot, A5 V-drop NR commit).
func (i *Instance) buildCommitForSigmaEligibility(eligibleVRoot [32]byte) (*Commit, error) {
	const layer = 0
	vLocal, hasVLocal := i.vLocalAtLayer(layer)
	if hasVLocal && ValueRoot(vLocal) == eligibleVRoot {
		valid, recorded := i.HostValidity(layer, vLocal)
		if recorded && valid {
			return i.buildCommitSigned(vLocal)
		}
	}
	return i.buildCommitNR()
}

// buildCommitSigned constructs and records a KindCommit-Signed on V at
// L_0. Locks EKM σ, signs the partial, populates the threshold pool,
// and caches ownCommit. Returns the built Commit.
func (i *Instance) buildCommitSigned(v Value) (*Commit, error) {
	const layer = 0
	if err := i.transitionToSigma(layer, v); err != nil {
		return nil, fmt.Errorf("twoab: σ-emit at L_0: %w", err)
	}
	partial, cached := i.ownPartials[layer]
	if !cached {
		p, err := i.signer.SignPartial(v)
		if err != nil {
			return nil, fmt.Errorf("twoab: sign σ at L_0: %w", err)
		}
		i.ownPartials[layer] = p
		partial = p
	}
	c := &Commit{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    append(Value{}, v...),
		L0Partial:  partial,
	}
	i.ownCommit = c
	// Self-populate the L_0 σ threshold pool.
	i.addToSigmaPool(layer, ValueRoot(v), i.ownOperatorID, partial)
	return c, nil
}

// buildCommitNR constructs and records a KindCommit-NR at L_0. Locks
// EKM NR, signs the nr_tag_0 partial, populates the threshold pool, and
// caches ownCommit.
func (i *Instance) buildCommitNR() (*Commit, error) {
	const layer = 0
	if err := i.transitionToNR(layer); err != nil {
		return nil, fmt.Errorf("twoab: NR-emit at L_0: %w", err)
	}
	tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, layer)
	sig, err := i.tagSigner.SignPartial(tag)
	if err != nil {
		return nil, fmt.Errorf("twoab: sign nr_tag_0: %w", err)
	}
	c := &Commit{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  sig,
	}
	i.ownCommit = c
	// Self-populate the L_0 nr_tag pool.
	i.addToNoValuePool(layer, i.ownOperatorID)
	i.addToNrTagPool(layer, i.ownOperatorID, sig)
	return c, nil
}

// ObserveCommit records a peer's (or the local operator's own, after
// broadcast) Phase-2b (or Phase-2a NRDirect) Commit. Per spec §Phase 2b:
//
//   - Bundles must pass structural validation (cluster id, slot, sender
//     in cluster, valid Side flag, well-formed L0Value/L0Partial/LayerEntries
//     per Side).
//   - First Commit observed from op → recorded in peerCommit, pool
//     updates per inference rules. KindCommit-Signed implies an earlier
//     KindValue existed (per A2/A6) — receivers infer pool membership.
//   - Identical re-broadcast → silent dedup.
//   - Distinct second Commit (different Side or different L0Value) →
//     Rule 6a + (potentially) Rule 3 / Rule 1.
//   - Cross-side observation: σ + NR partials from same op at same layer
//     → Rule 1 (CrossSigning).
//
// Self-observation: silent dedup (own emissions are self-pool-updated).
func (i *Instance) ObserveCommit(c *Commit) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidateCommit(c, i.cfg); err != nil {
		return err
	}
	op := c.OperatorID
	if op == i.ownOperatorID {
		return nil
	}
	const layer = 0
	existing, hadCommit := i.peerCommit[op]
	if hadCommit {
		if commitContentHash(existing) == commitContentHash(c) {
			return nil // identical re-broadcast
		}
		// Distinct second Commit from same op — Rule 6a. Sub-types:
		//   - same side, different L0Value (cross-σ-V) → also Rule 3
		//   - different side (Signed vs NR) → also Rule 1 (cross-signing)
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      layer,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					CommitA: existing,
					CommitB: deepCopyCommit(c),
				},
			})
		}
		if existing.Side == CommitSideSigned && c.Side == CommitSideSigned &&
			!bytes.Equal(existing.L0Value, c.L0Value) && i.recordRule3(op, layer) {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossCommitEquivocation,
				OperatorID: op,
				Layer:      layer,
				CrossCommitEquivocation: &CrossCommitEquivocationEvidence{
					ValueA:   append(Value{}, existing.L0Value...),
					ValueB:   append(Value{}, c.L0Value...),
					PartialA: append(Signature{}, existing.L0Partial...),
					PartialB: append(Signature{}, c.L0Partial...),
				},
			})
		}
		if existing.Side != c.Side && (existing.Side == CommitSideSigned || c.Side == CommitSideSigned) &&
			i.recordRule1(op, layer) {
			var sigmaSide *Commit
			var nrSide *Commit
			if existing.Side == CommitSideSigned {
				sigmaSide = existing
				nrSide = c
			} else {
				sigmaSide = c
				nrSide = existing
			}
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossSigning,
				OperatorID: op,
				Layer:      layer,
				CrossSigning: &CrossSigningEvidence{
					SigmaPartial: append(Signature{}, sigmaSide.L0Partial...),
					SigmaValue:   append(Value{}, sigmaSide.L0Value...),
					NRPartial:    append(Signature{}, nrSide.L0Partial...),
				},
			})
		}
		return nil
	}

	// First Commit from this op.
	hadValue := i.peerValueMsg[op]
	hadNoValue := i.peerNoValueMsg[op]

	// Detect non-conformant emission sequences (Rule 6a, sequence-only).
	//
	// Per spec §Receiver ordering tolerance + §Pool aggregation rules /
	// Key inference: KindCommit-Signed implies a KindValue existed (per
	// A2/A6). Even if peerValueMsg[op] is nil, we infer the upgrade
	// happened — KindValue may have been lost in propagation, or may
	// arrive later via gossipsub reflood. We do NOT slash the
	// `KindNoValue → KindCommit-Signed` (no observed upgrade) case
	// because the receiver can't distinguish it from an in-flight A6
	// sequence; the inference defaults to A6.
	//
	// The only unambiguously-slashable post-Phase-2a sequence detectable
	// here is `Side=NRDirect after any prior Phase 2a emission` (A8
	// requires NRDirect to be the sole emission).
	if c.Side == CommitSideNRDirect && (hadValue != nil || hadNoValue != nil) {
		if i.recordRule6a(op) {
			ev := Phase2EquivocationEvidence{CommitB: deepCopyCommit(c)}
			if hadValue != nil {
				ev.ValueA = hadValue
			} else {
				ev.NoValueA = hadNoValue
			}
			i.recordEvidence(Evidence{
				Rule:               EvidencePhase2Equivocation,
				OperatorID:         op,
				Layer:              layer,
				Phase2Equivocation: &ev,
			})
		}
	}

	i.peerCommit[op] = deepCopyCommit(c)

	switch c.Side {
	case CommitSideSigned:
		// Inferred: an earlier KindValue existed (A2/A6). Add op to
		// valuePool[0][V_root] via inference.
		i.addToValuePool(layer, ValueRoot(c.L0Value), op)
		// Threshold pool: add the verified σ partial.
		if i.verifyL0SigmaPartial(op, c.L0Value, c.L0Partial) {
			i.addToSigmaPool(layer, ValueRoot(c.L0Value), op, c.L0Partial)
		} else if i.recordRule5Fired(op, layer) {
			// Rule 5: fake plaintext σ at L_0.
			i.recordEvidence(Evidence{
				Rule:       EvidenceFakePlaintextSigma,
				OperatorID: op,
				Layer:      layer,
				FakePlaintextSigma: &FakePlaintextSigmaEvidence{
					OnionPartial:        append(Signature{}, c.L0Partial...),
					OnionValue:          append(Value{}, c.L0Value...),
					RetainedValueHashes: i.retainedL0ValueHashes(),
				},
			})
		}
	case CommitSideNR:
		// NR-direction at L_0.
		i.addToNoValuePool(layer, op)
		i.addToNrTagPool(layer, op, c.L0Partial)
	case CommitSideNRDirect:
		// Phase-2a NRDirect: same L_0 NR semantics, plus the L_k>0
		// LayerEntries contribute to deeper-layer pools.
		i.addToNoValuePool(layer, op)
		i.addToNrTagPool(layer, op, c.L0Partial)
		i.processObservedLayerEntries(op, c.LayerEntries)
	}

	i.afterStateDelta()
	return nil
}

// verifyL0SigmaPartial returns true if the σ partial verifies against
// op's pubshare on the claimed V. False on signer-verify failure or
// missing pubshare.
func (i *Instance) verifyL0SigmaPartial(op OperatorID, v Value, partial Signature) bool {
	opPub, ok := i.pubKeyShares[op]
	if !ok || len(opPub) == 0 {
		return false
	}
	return i.signer.VerifyPartial(opPub, v, partial)
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

// deepCopyCommit returns an independent copy of c.
func deepCopyCommit(c *Commit) *Commit {
	if c == nil {
		return nil
	}
	out := *c
	out.L0Value = append(Value{}, c.L0Value...)
	out.L0Partial = append(Signature{}, c.L0Partial...)
	out.LayerEntries = cloneLayerEntries(c.LayerEntries)
	return &out
}
