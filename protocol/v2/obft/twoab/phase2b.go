package twoab

import (
	"errors"
	"fmt"
)

// Phase 2b dynamic commit emission. Per spec §Phase 2b:
//
// Each operator emits at most one KindCommit per slot. Emission is
// trigger-driven (NO T_commit hard wall) — the Instance is a passive
// state machine that evaluates TWO triggers in priority order on every
// state delta:
//
//   1. Equivocation trigger (highest priority): op retained ≥ 2 distinct
//      V_0 from the L_0 leader AND has not yet emitted KindValue (still
//      in EKM coordination state at L_0) → emit KindCommit-NRDirect at
//      Phase 2a (handled in MaybeFirePhase2a). Post-σ-lock, equivocation
//      is unrecoverable — the op is committed to V via its σ partial
//      already on the wire in KindValue. See docs/2abOBFT.md §Failure modes.
//   2. NR-eligibility trigger: cluster's noValuePool[L_0] reaches qEnc
//      AND op cannot σ at L_0 (cannot-σ gate) → KindCommit-NR. The gate
//      blocks σ-eligible ops from forfeiting σ contributions to premature
//      NR-side emission. The "cannot σ" semantic is evaluated against
//      witness-derived L0Ready (host-validated V from either direct
//      bundle or harvest), not just bundle retention.
//
// There is no σ-eligibility trigger: the σ-side terminal emission is
// carried inline in KindValue (with the emitter's σ partial), with no
// separate Phase-2b σ-commit event. Receivers still run Resolve()
// opportunistically on σ-pool growth — once cluster σ-pool[V_0] reaches
// qV (via direct peer KindValues + leader LWitness + harvested forwarded-
// witness contributions), the L_0 σ-quorum reconstruction succeeds without
// any commit-trigger firing locally.
//
// Per spec §Emission ordering, the upgrade check (Phase 2a-late) runs
// BEFORE commit trigger evaluation on every state delta.

// afterStateDelta is the per-tick processing cascade invoked at the end
// of every Observe* method (and ApplyHostValidity). Implements the
// normative emission ordering from spec §Emission ordering:
//
//  1. Run MaybeBuildAndBroadcastUpgrade — the upgrade if preconditions
//     are now satisfied.
//  2. Run MaybeBuildAndBroadcastCommit — NR-eligibility trigger
//     evaluation (there is no σ-eligibility trigger, and the
//     equivocation trigger fires at Phase 2a only via MaybeFirePhase2a,
//     not here).
//
// Upgrade-first ensures the upgrade sequence fires correctly when a
// NoValueMsg-path op observes V_0 + host valid via reflood/harvest — the
// upgrade KindValue carries the σ partial directly and is the σ-side
// terminal emission. Each helper is independently idempotent: returns
// early when conditions aren't met or when the relevant emission has
// already fired.
//
// afterStateDelta is idempotent and safe to call repeatedly. After the
// op has emitted its single Commit, subsequent calls are no-ops.
//
// Errors from the cascade helpers (genuine signer/EKM failures, not the
// preconditions-not-met ErrUpgradeNotAvailable sentinel) are stashed on
// the Instance and surfaced via CascadeErrors(). Without this, a silent
// BLS hiccup eating the cascade would be invisible until the slot
// misses for unclear reasons.
func (i *Instance) afterStateDelta() {
	if _, err := i.MaybeBuildAndBroadcastUpgrade(); err != nil && !errors.Is(err, ErrUpgradeNotAvailable) {
		i.recordCascadeError(err)
	}
	if _, err := i.MaybeBuildAndBroadcastCommit(); err != nil {
		i.recordCascadeError(err)
	}
}

// cascadeErrorsCap bounds the accumulator. In practice a slot processes
// O(10) state deltas with 0–2 cascade-error contributions each, so the
// realistic upper bound is ~20. The cap is generous — only a pathological
// signer that fails every tick forever would hit it — and prevents
// unbounded memory growth in such a degenerate case. Errors past the cap
// are silently dropped; the Stats().CascadeErrorsCapped flag surfaces
// the truncation event for callers that want to detect it.
const cascadeErrorsCap = 100

// recordCascadeError appends err to cascadeErrors with a length cap.
// The cap-hit case sets the truncation flag (visible via Stats) and
// drops the new error silently.
func (i *Instance) recordCascadeError(err error) {
	if len(i.cascadeErrors) >= cascadeErrorsCap {
		i.cascadeErrorsCapped = true
		return
	}
	i.cascadeErrors = append(i.cascadeErrors, err)
}

// MaybeBuildAndBroadcastCommit evaluates the Phase-2b commit triggers
// and, if one fires AND the op has not yet emitted a KindCommit, builds
// + records the appropriate KindCommit-NR. Idempotent.
//
// The only commit triggered here is KindCommit-NR; the σ-side terminal
// emission is carried inline in KindValue (handled by MaybeFirePhase2a
// + MaybeBuildAndBroadcastUpgrade). The equivocation trigger fires only
// for ops still in EKM coordination state at L_0 (haven't yet emitted
// KindValue with a σ partial). Post-σ-lock equivocation is unrecoverable.
//
// Preconditions for emission:
//   - Op has emitted KindNoValue at Phase 2a (ownNoValueMsg non-nil).
//     KindValue-path ops are σ-locked at L_0 and can't transition to
//     NR-side (would equivocate the σ partial already on the wire).
//     NRDirect-path ops (ownCommit set at Phase 2a) are a sole emission
//     and don't get a second Commit.
//   - Op has not already emitted KindCommit (ownCommit nil).
//
// Returns:
//   - (*Commit, nil) on successful emission. Caller broadcasts.
//   - (nil, nil) if no trigger fires (silent wait — the slot's relay-
//     submission deadline at the runner level is the only hard wall).
//   - (nil, err) on internal build failure.
//
// Return-shape convention vs MaybeBuildAndBroadcastUpgrade: Commit returns
// (nil, nil) for "no eligibility right now" because the trigger predicate
// is a passive observation of cluster state and the no-trigger state is
// the silent default of partial-synchrony waiting. Upgrade, by contrast,
// returns the explicit ErrUpgradeNotAvailable sentinel because it has a
// single precondition cluster that callers may want to distinguish from
// "succeeded silently". See MaybeBuildAndBroadcastUpgrade's docstring
// for the full rationale.
func (i *Instance) MaybeBuildAndBroadcastCommit() (*Commit, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return nil, ErrInstanceEnded
	}
	if i.ownCommit != nil {
		return nil, nil // already emitted
	}
	if i.ownNoValueMsg == nil {
		// Phase 2a hasn't fired KindNoValue. Either:
		//   - Phase 2a not yet fired (silent wait).
		//   - Phase 2a fired KindValue (σ-locked; no Commit follows).
		//   - Phase 2a fired KindCommit-NRDirect (ownCommit non-nil, caught above).
		return nil, nil
	}

	const layer = 0

	// NR-eligibility trigger (the only trigger here).
	// Cannot-σ gate: ops who CAN still σ at L_0 must take the upgrade
	// path (via MaybeBuildAndBroadcastUpgrade) rather than NR-pivot from
	// KindNoValue. The gate fires for ops genuinely unable to σ — no V
	// retained, retention but no host-valid verdict, or equivocation
	// observed (≥ 2 retained).
	if i.noValuePoolSize(layer) >= i.cfg.QEnc() && !i.canSigmaAtLayer(layer) {
		return i.buildCommitNR()
	}

	// No trigger fired.
	return nil, nil
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
// broadcast) Phase-2b (or Phase-2a NRDirect) KindCommit. Commit is
// NR-side only (CommitSideNR / CommitSideNRDirect); the σ-side rides
// inline in KindValue.
//
//   - Commit must pass structural validation (cluster id, slot, sender
//     in cluster, NR or NRDirect side, well-formed L0Partial/LayerEntries).
//   - First Commit observed from op → recorded in peerCommit, NR partial
//     verified and pooled into nrTagPool[0] on success; noValuePool[0]
//     also grows.
//   - Identical re-broadcast → silent dedup.
//   - Distinct second Commit (different L0Partial bytes — usually different
//     LayerEntries on NRDirect) → Rule 6a (Phase-2 equivocation).
//   - Prior KindValue + this KindCommit-NR/NRDirect from same op → Rule 1
//     (cross-signing: σ partial in the prior KindValue's L0Partial XOR NR
//     partial in this Commit's L0Partial — the EKM σ-XOR-NR invariant is
//     violated).
//   - Prior KindNoValue + this KindCommit-NRDirect → Rule 6a
//     (Commit-NRDirect must be the sole emission).
//   - Prior KindValue + this KindCommit-NRDirect → Rule 6a AND Rule 1.
//
// Self-observation: silent dedup (own emissions are self-pool-updated).
func (i *Instance) ObserveCommit(c *Commit) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
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
		// Distinct second Commit from same op — Rule 6a. (Both commits
		// are NR-side; cross-V detection between commits is moot since
		// neither carries V. The only meaningful distinction is
		// NRDirect-with-different-LayerEntries.)
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
		return nil
	}

	// First Commit from this op.
	hadValue := i.peerValueMsg[op]
	hadNoValue := i.peerNoValueMsg[op]

	// Detect Rule 1 (cross-signing) when a prior KindValue (σ-side) is
	// followed by this KindCommit (NR-side). The σ partial sits in
	// hadValue.L0Partial; the NR partial sits in c.L0Partial. Both must
	// verify against their respective key shares for the cryptographic
	// proof to land — a forged partial on either side fires Rule 5
	// instead (separately, downstream).
	if hadValue != nil &&
		i.verifySigmaPartial(op, hadValue.V, hadValue.L0Partial) &&
		i.verifyNRTagPartial(op, layer, c.L0Partial) &&
		i.recordRule1(op, layer) {
		i.recordEvidence(Evidence{
			Rule:       EvidenceCrossSigning,
			OperatorID: op,
			Layer:      layer,
			CrossSigning: &CrossSigningEvidence{
				SigmaPartial: append(Signature{}, hadValue.L0Partial...),
				SigmaValue:   append(Value{}, hadValue.V...),
				NRPartial:    append(Signature{}, c.L0Partial...),
			},
		})
	}

	// Detect non-conformant emission sequences (Rule 6a, sequence-only).
	//
	// The authorized set (spec §Slashing evidence, Rule 6):
	//   - NoValue→Value (the upgrade; handled in ObserveValueMsg).
	//   - NoValue→Commit-NR (authorized; no Rule 6a here).
	//   - Commit-NRDirect-alone (no prior Phase-2a emission).
	//
	// Sequences detected here:
	//   - hadValue + KindCommit-NR/NRDirect: Rule 6a (cross-σ-NR violation
	//     of σ-XOR-NR — also Rule 1 above on cryptographic verify).
	//   - hadNoValue + KindCommit-NRDirect: Rule 6a (Commit-NRDirect must
	//     be the sole emission; prior KindNoValue voids that).
	switch {
	case c.Side == CommitSideNR && hadValue != nil:
		// KindValue (σ-side) followed by KindCommit-NR is unauthorized:
		// the σ-lock acquired at KindValue emit blocks the NR-pivot.
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      layer,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					ValueA:  hadValue,
					CommitB: deepCopyCommit(c),
				},
			})
		}
	case c.Side == CommitSideNRDirect && (hadValue != nil || hadNoValue != nil):
		// Commit-NRDirect must be the sole emission; any prior Phase-2a
		// emission violates the rule.
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
	case CommitSideNR:
		// NR-direction at L_0. Verify the nr_tag_0 partial before
		// adding to the threshold pool — a byz spamming garbage NR
		// partials would otherwise pollute nr_tag_0-pool and cause
		// Phase-3 chain-decryption aggregation to fail or compute a
		// wrong key.
		if i.verifyNRTagPartial(op, layer, c.L0Partial) {
			i.addToNoValuePool(layer, op)
			i.addToNrTagPool(layer, op, c.L0Partial)
		}
		// On verify failure: claim-pool (noValuePool) is also skipped —
		// without a valid partial the op didn't actually NR-commit
		// cryptographically; the L_0 claim has no signature to back it.
		// Receivers ignore the bogus commit entirely.
	case CommitSideNRDirect:
		// Phase-2a NRDirect: same L_0 NR semantics, plus the L_k>0
		// LayerEntries contribute to deeper-layer pools.
		if i.verifyNRTagPartial(op, layer, c.L0Partial) {
			i.addToNoValuePool(layer, op)
			i.addToNrTagPool(layer, op, c.L0Partial)
		}
		i.processObservedLayerEntries(op, c.LayerEntries)
	}

	i.afterStateDelta()
	return nil
}

// verifySigmaPartial returns true if the σ partial verifies against
// op's pubshare on the claimed V. False on signer-verify failure or
// missing pubshare.
func (i *Instance) verifySigmaPartial(op OperatorID, v Value, partial Signature) bool {
	opPub, ok := i.pubKeyShares[op]
	if !ok || len(opPub) == 0 {
		return false
	}
	return i.signer.VerifyPartial(opPub, v, partial)
}

// retainedValueHashes returns sha256 hashes of all V's retained at the
// given layer (across all leaders, though in practice only one leader per
// layer). Used to populate Rule 5 evidence with the receiver's reference
// set for third-party verification. Layer-general so per-layer
// leader-witness Rule 5 carries the correct layer's reference set.
func (i *Instance) retainedValueHashes(layer int) [][]byte {
	leaderMap := i.retainedBundles[layer]
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
	out.L0Partial = append(Signature{}, c.L0Partial...)
	out.LayerEntries = cloneLayerEntries(c.LayerEntries)
	return &out
}
