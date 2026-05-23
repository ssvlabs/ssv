package twoab

import (
	"bytes"
	"fmt"
)

// ApplyHostValidity records the host application's valid / not-valid
// verdict on the given V at the given layer. Per spec §Phase 2a, the host
// is consulted at:
//
//   - Phase-2a fire-time: to determine whether the op emits KindValue
//     with σ partial (host says valid AND op has V; the σ-lock is
//     acquired at this moment) or KindNoValue (otherwise).
//   - Upgrade time: a KindNoValue-path op that later observes V_0 + host
//     valid emits the upgrade KindValue (also carrying its σ partial). The
//     runner re-asks the host on the harvested V if it hasn't validated
//     this (layer, V) yet — see WantsHostValidationCh.
//
// σ-lock is acquired at KindValue emit time, so once the op is σ-locked a
// mid-slot host re-validate-NV verdict has no protocol effect (flipping to
// NR would self-equivocate).
//
// Idempotent for the same (layer, V, valid) triple. Calling with a
// different `valid` value for the same (layer, V) overwrites — the
// most recent host verdict wins, consistent with the spec's intent
// that host stabilization narrows the divergence window over time.
//
// Divergence from base: base's ApplyHostValidity REJECTS a verdict-
// flip with an error (`obft: host validity verdict for layer N
// already locked`). twoab tolerates the flip silently. This is
// intentional — base treats host validity as a commit-time decision
// fixed before σ-commitment; twoab treats it as a flowing signal that
// can shift as the host re-evaluates the candidate V mid-slot (the
// runner is expected to call ApplyHostValidity opportunistically as
// the host re-derives validity, not once-per-slot at Phase 1). See
// docs/OBFT-TWOAB-CONVERGENCE-PLAN.md §C4 for the convergence-audit
// note on this divergence.
//
// On L_0 host-validity updates, the Instance runs the per-tick
// processing cascade so a host-flip-to-valid can trigger the upgrade
// path (if the op is on KindNoValue path AND now has V_0 + host says
// valid).
//
// Returns an error if `layer` is out of [0, K) or `value` is empty.
//
// Post-Finalize guard: returns ErrInstanceEnded if Finalize has been
// called. ApplyHostValidity is typically invoked from an async host-
// reply path (the host receives a ValidationRequest on
// wantsHostValidationCh, runs its verdict logic, then calls back into
// the Instance). The runner adapter SHOULD gate host callbacks on the
// Instance's Finalized state, but async-reply races are easy to miss in
// integration code — and unlike the Observe* methods (which the runner
// owns the dispatch for), the host-reply path crosses the runner ↔
// host process boundary where the runner can't always intercept in
// time. The ErrInstanceEnded return lets late callers detect the late-
// reply scenario explicitly; runners that want silent-ignore semantics
// can swallow the error at the callback boundary.
//
// (Post Phase 3 convergence — see docs/OBFT-TWOAB-CONVERGENCE-PLAN.md
// §C1 — twoab's lifecycle mutators uniformly return ErrInstanceEnded
// post-Finalize, matching base.)
func (i *Instance) ApplyHostValidity(layer int, value Value, valid bool) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if layer < 0 || layer >= i.cfg.K() {
		return fmt.Errorf("twoab: %w: layer %d outside [0, %d)",
			ErrLayerOutOfRange, layer, i.cfg.K())
	}
	if len(value) == 0 {
		return ErrEmptyValue
	}
	if i.hostVerdict[layer] == nil {
		i.hostVerdict[layer] = make(map[string]bool)
	}
	root := ValueRoot(value)
	i.hostVerdict[layer][string(root[:])] = valid
	// Clear any in-flight pending validation flag for (layer, V_root) —
	// the verdict is now recorded; a subsequent harvest of the same V
	// would dedup on hostVerdict anyway, but clearing keeps the
	// pendingValidation set tight (memory-bounded).
	i.host.ClearPending(layer, root)
	i.afterStateDelta()
	// Recording a host verdict can flip computeLocalValueState to Value
	// (retained V_0 now host-valid). Signal L0Ready so the runner/DES can
	// async-fire MaybeFirePhase2a before TPhase2a.
	i.maybeSignalL0Ready()
	return nil
}

// HostValidity returns (valid, recorded) for the given (layer, V) pair.
// `recorded` is false if ApplyHostValidity has never been called for
// this (layer, V); `valid` is meaningless in that case.
func (i *Instance) HostValidity(layer int, value Value) (valid bool, recorded bool) {
	verdicts := i.hostVerdict[layer]
	if verdicts == nil {
		return false, false
	}
	root := ValueRoot(value)
	v, ok := verdicts[string(root[:])]
	return v, ok
}

// localValueState discriminates the three possible Phase-2a emission
// kinds. Internal helper for MaybeFirePhase2a.
type localValueState int

const (
	// localValueStateValue: op has V_0 retained + host valid at L_0.
	// Emit KindValue.
	localValueStateValue localValueState = iota
	// localValueStateNoValue: op has no V_0 OR host says NV at L_0.
	// Emit KindNoValue.
	localValueStateNoValue
	// localValueStateNRDirect: op observed L_0 equivocation
	// (≥ 2 distinct V_0 retained from leader). Emit KindCommit-NRDirect.
	localValueStateNRDirect
)

// computeLocalValueState applies the Phase-2a fire-time decision rule
// to determine this operator's emission kind at L_0. Per spec §Phase 2a:
//
//   - L_0 retained ≥ 2 distinct V's → NRDirect (equivocation observed).
//   - L_0 retained exactly 1 V AND host says valid for that V → Value.
//   - L_0 retained exactly 1 V AND host says not-valid for that V → NoValue.
//   - L_0 retained 0 V's → NoValue.
//   - L_0 retained 1 V but host hasn't been consulted yet → NoValue
//     (defensive — Phase-2a fire-time host-not-consulted-yet is a
//     timing race, treated as NV for emission purposes).
func (i *Instance) computeLocalValueState() localValueState {
	const layer = 0
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]

	// Equivocation threshold: `>= 2` distinct V_0 retained from leader
	// triggers NRDirect emission. The literal `2` here is intentionally
	// NOT the MaxRetainedPerOpLayer constant — those are correlated
	// (retention is capped at MaxRetainedPerOpLayer, so the equivocation
	// threshold can never exceed it) but semantically distinct: the
	// retention cap is a memory-bound policy, the equivocation threshold
	// is a protocol-detection predicate. Conflating the two via the
	// constant would obscure the protocol intent.
	if len(retained) >= 2 {
		return localValueStateNRDirect
	}
	if len(retained) == 0 {
		return localValueStateNoValue
	}
	single := retained[0]
	valid, recorded := i.HostValidity(layer, single.Bundle.Value)
	if !recorded || !valid {
		return localValueStateNoValue
	}
	return localValueStateValue
}

// buildLayerEntries constructs the K-1 LayerEntries for L_1..L_{K-1}
// at the local op's Phase-2a fire-time. Each entry's kind is determined
// by the op's retention + host-validity state at that layer:
//
//   - SigmaChained if op has V_k retained AND host says valid (and not
//     equivocation-observed).
//   - NRPlaintext if op cannot σ at L_k (no V, host NV, or equivocation)
//     AND k < K-1 (no nr_tag at deepest layer).
//   - Empty otherwise (k = K-1 NR-side, no nr_tag exists).
//
// For SigmaChained: sign the σ partial on V_k via signer (V-keypair share),
// chain-encrypt under nr_tag_0..nr_tag_{k-1}, and EKM-lock σ at layer k.
//
// For NRPlaintext: sign the nr_tag_k tag via tagSigner (IBE-keypair share),
// and EKM-lock NR at layer k.
//
// The K-1 returned entries are ordered Layer=1, 2, ..., K-1.
func (i *Instance) buildLayerEntries() ([]LayerEntry, error) {
	K := i.cfg.K()
	entries := make([]LayerEntry, 0, K-1)
	for k := 1; k < K; k++ {
		entry, err := i.buildLayerEntry(k)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// buildForwardedWitnesses assembles the Witnesses section for a KindValue:
// the L_0 leader's witness (always — KindValue is the L_0 σ-side emission)
// plus, for each layer the emitter is σ-side on (a SigmaChained entry in
// `entries`), that layer's leader witness.
//
// Each witness is pulled from the emitter's own retained leader bundle at
// that layer (retainedBundles[k][leader].LeaderSigma); being σ-side at k
// implies the emitter retained that bundle with the leader's verified
// witness. Per the "root plus σ-colocation" design, V rides alongside
// (v.V for L_0, the SigmaChained entry for k>0), so the witness carries
// only the value-root — receivers recover V from the same KindValue.
//
// `l0Root` is ValueRoot(v.V) for the KindValue being built; `entries` are
// its K-1 LayerEntries.
func (i *Instance) buildForwardedWitnesses(l0Root [32]byte, entries []LayerEntry) []LayerWitness {
	out := make([]LayerWitness, 0, 1+len(entries))
	// L_0 — always present (the op is σ-at-L_0 by virtue of emitting KindValue).
	l0Leader := i.cfg.Layers[0].Leader
	if r := i.retainedBundles[0][l0Leader]; len(r) > 0 && len(r[0].Bundle.LeaderSigma) > 0 {
		out = append(out, LayerWitness{
			Layer:     0,
			ValueRoot: l0Root,
			Witness:   append(Signature{}, r[0].Bundle.LeaderSigma...),
		})
	}
	// k>0 — one per SigmaChained entry (σ-side layer); V rides in the entry.
	for _, e := range entries {
		if e.Kind != LayerEntrySigmaChained {
			continue
		}
		kLeader := i.cfg.Layers[e.Layer].Leader
		r := i.retainedBundles[e.Layer][kLeader]
		if len(r) == 0 || len(r[0].Bundle.LeaderSigma) == 0 {
			continue
		}
		out = append(out, LayerWitness{
			Layer:     e.Layer,
			ValueRoot: ValueRoot(e.V),
			Witness:   append(Signature{}, r[0].Bundle.LeaderSigma...),
		})
	}
	return out
}

// buildLayerEntry constructs a single LayerEntry at layer k > 0.
func (i *Instance) buildLayerEntry(k int) (LayerEntry, error) {
	if k <= 0 || k >= i.cfg.K() {
		return LayerEntry{}, fmt.Errorf("twoab: %w: layer %d outside (0, %d)",
			ErrLayerOutOfRange, k, i.cfg.K())
	}
	// If we already σ-committed at this layer, stay consistent — emit
	// SigmaChained on the locked V regardless of the current host verdict.
	// The only way to be σ-locked at k>0 before Phase-2a is as this layer's
	// leader via our own Phase-1 LeaderSigma (BuildPhase1Bundle σ-locks at
	// witness-sign time). Flipping to NR here would publish both a σ witness
	// (in our Phase-1 bundle) and an NR partial at the same layer —
	// self-equivocation. This mirrors the L_0 principle that a host
	// re-validate-NV verdict has no protocol effect once the op is σ-locked
	// (see ApplyHostValidity docstring).
	if i.sigmaLocked[k] {
		return i.buildSigmaChainedEntry(k, i.sigmaLockedV[k])
	}
	leaderID := i.cfg.Layers[k].Leader
	retained := i.retainedBundles[k][leaderID]

	// Equivocation observed at L_k → NR-side at this layer.
	// (Inline `2` not MaxRetainedPerOpLayer — same rationale as
	// computeLocalValueState: the equivocation-detection threshold is
	// a protocol predicate distinct from the retention-cap policy.)
	if len(retained) >= 2 {
		return i.buildNRPlaintextEntry(k)
	}
	// No retained V at L_k → NR-side.
	if len(retained) == 0 {
		return i.buildNRPlaintextEntry(k)
	}
	// Exactly 1 retained V — consult host.
	vK := retained[0].Bundle.Value
	valid, recorded := i.HostValidity(k, vK)
	if !recorded || !valid {
		return i.buildNRPlaintextEntry(k)
	}
	// σ-side at L_k.
	return i.buildSigmaChainedEntry(k, vK)
}

// buildSigmaChainedEntry builds a SigmaChained LayerEntry at layer k > 0
// on V. Signs the σ partial, chain-encrypts it, and locks EKM σ at layer k.
func (i *Instance) buildSigmaChainedEntry(k int, v Value) (LayerEntry, error) {
	if err := i.transitionToSigma(k, v); err != nil {
		return LayerEntry{}, fmt.Errorf("twoab: σ-emit at layer %d: %w", k, err)
	}
	partial, cached := i.ownPartials[k]
	if !cached {
		p, err := i.signer.SignPartial(v)
		if err != nil {
			return LayerEntry{}, fmt.Errorf("twoab: sign σ at layer %d: %w", k, err)
		}
		i.ownPartials[k] = p
		partial = p
	}
	ct, err := i.chainEncryptForLayer(k, partial)
	if err != nil {
		return LayerEntry{}, fmt.Errorf("twoab: encrypt at layer %d: %w", k, err)
	}
	// Self-populate the threshold pool: at L_k>0, our σ partial is
	// available locally (we just signed it). Peers' encrypted bytes will
	// be decrypted by Resolve() during the chain-decryption walk.
	vRoot := ValueRoot(v)
	i.addToValuePool(k, vRoot, i.ownOperatorID)
	i.addToSigmaPool(k, vRoot, i.ownOperatorID, partial)
	return LayerEntry{
		Layer:   k,
		Kind:    LayerEntrySigmaChained,
		V:       append(Value{}, v...),
		Payload: ct,
	}, nil
}

// buildNRPlaintextEntry builds an NRPlaintext LayerEntry at layer k.
// At the deepest layer (k = K-1) there is no nr_tag, so the entry is
// LayerEntryEmpty (the op still EKM-locks NR for σ-XOR-NR enforcement,
// but emits no wire bytes for this layer).
func (i *Instance) buildNRPlaintextEntry(k int) (LayerEntry, error) {
	if err := i.transitionToNR(k); err != nil {
		return LayerEntry{}, fmt.Errorf("twoab: NR-emit at layer %d: %w", k, err)
	}
	if k >= i.cfg.K()-1 {
		// Deepest layer — no nr_tag to sign; emit empty entry.
		return LayerEntry{
			Layer: k,
			Kind:  LayerEntryEmpty,
		}, nil
	}
	tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, k)
	sig, err := i.tagSigner.SignPartial(tag)
	if err != nil {
		return LayerEntry{}, fmt.Errorf("twoab: sign NR partial at layer %d: %w", k, err)
	}
	// Self-populate the pools.
	i.addToNoValuePool(k, i.ownOperatorID)
	i.addToNrTagPool(k, i.ownOperatorID, sig)
	return LayerEntry{
		Layer:   k,
		Kind:    LayerEntryNRPlaintext,
		Payload: sig,
	}, nil
}

// MaybeFirePhase2a fires the local operator's Phase-2a emission at the
// fire-instant T_phase_2a. Per spec §Phase 2a, every operator emits
// exactly one of {KindValue, KindNoValue, KindCommit-NRDirect} based on
// their local state at fire-time.
//
// Idempotent: subsequent calls return the cached (ValueMsg, NoValueMsg,
// Commit) triple — same as the first-call return shape. The local
// emission is whatever was set on first call; callers can equally
// retrieve it via OwnValueMsg / OwnNoValueMsg / OwnCommit.
//
// Returns a triple {ValueMsg, NoValueMsg, Commit, error} — exactly one
// of the message-typed fields is non-nil on success (matching the
// local value state at fire-time). Returns ErrInstanceEnded
// post-Finalize. Returns a wrapped error if Phase 2a build fails (e.g.,
// signer error during LayerEntry construction).
func (i *Instance) MaybeFirePhase2a() (*ValueMsg, *NoValueMsg, *Commit, error) {
	if i == nil {
		return nil, nil, nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return nil, nil, nil, ErrInstanceEnded
	}
	if i.phase2aFired {
		return i.ownValueMsg, i.ownNoValueMsg, i.ownCommit, nil
	}
	state := i.computeLocalValueState()
	entries, err := i.buildLayerEntries()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("twoab: build LayerEntries: %w", err)
	}
	// Note: phase2aFired is set AFTER each branch's irreversible side-
	// effects complete, not before. A failure mid-branch (e.g. signer
	// error) returns an error AND leaves the flag false so the caller
	// can retry. Setting too early would silently wedge the slot — the
	// fast-path at line 242 would return (nil, nil, nil, nil) on retry.
	switch state {
	case localValueStateValue:
		const layer = 0
		retained := i.retainedBundles[layer][i.cfg.Layers[layer].Leader]
		v := retained[0].Bundle.Value
		root := ValueRoot(v)
		// Forward leader σ-witnesses (L_0 always, plus each σ-side deeper
		// layer) so receivers can harvest V + seed σ-pool with the leader's
		// head-start at every layer (peer-reflood-V). Assembled from
		// `entries` + retained bundles in the struct below.
		// Sign our own σ partial on V at L_0 and acquire the σ-side EKM lock
		// at L_0. KindValue is the terminal σ-side emission — there is no
		// follow-up σ commit. Sequence is:
		//   (1) Compute the partial (pure; signer may error before any
		//       state mutation — abort with no side effects on failure).
		//   (2) Acquire σ-lock at L_0 on V. Failure means EKM already
		//       locked the op on a different V or on NR; should not
		//       happen for honest fire-time semantics but enforce.
		//   (3) Commit cache + self-pool. Both succeeded — emit.
		// Mirrors the sign-first-then-lock ordering used in
		// BuildPhase1Bundle for the L_0 leader's LeaderSigma.
		l0Partial, cached := i.ownPartials[layer]
		if !cached {
			p, err := i.signer.SignPartial(v)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("twoab: sign σ at L_0 for KindValue: %w", err)
			}
			l0Partial = p
		}
		if err := i.transitionToSigma(layer, v); err != nil {
			return nil, nil, nil, fmt.Errorf("twoab: σ-lock at L_0 for KindValue: %w", err)
		}
		i.ownPartials[layer] = l0Partial
		i.addToSigmaPool(layer, root, i.ownOperatorID, l0Partial)
		i.ownValueMsg = &ValueMsg{
			ClusterID:    i.cfg.ClusterID,
			OperatorID:   i.ownOperatorID,
			Height:       i.cfg.Height,
			V:            append(Value{}, v...),
			ValueRoot:    root,
			Witnesses:    i.buildForwardedWitnesses(root, entries),
			L0Partial:    append(Signature{}, l0Partial...),
			LayerEntries: entries,
		}
		i.addToValuePool(layer, root, i.ownOperatorID)
		i.phase2aFired = true
		// Phase 2a fire is a state delta — run the per-tick cascade so
		// downstream triggers can fire immediately if cluster pool state
		// is already satisfied.
		i.afterStateDelta()
		return i.ownValueMsg, nil, nil, nil
	case localValueStateNoValue:
		i.ownNoValueMsg = &NoValueMsg{
			ClusterID:    i.cfg.ClusterID,
			OperatorID:   i.ownOperatorID,
			Height:       i.cfg.Height,
			LayerEntries: entries,
		}
		i.addToNoValuePool(0, i.ownOperatorID)
		i.phase2aFired = true
		i.afterStateDelta()
		return nil, i.ownNoValueMsg, nil, nil
	case localValueStateNRDirect:
		// Phase-2a NR-direct: equivocation observed at L_0. Emit a
		// Commit with Side=NRDirect that bundles the L_0 nr_tag_0
		// partial plus the K-1 LayerEntries (since the op skips KindValue
		// / KindNoValue entirely — the Commit-NRDirect-alone sequence).
		if err := i.transitionToNR(0); err != nil {
			return nil, nil, nil, fmt.Errorf("twoab: NR-emit at L_0 (NRDirect): %w", err)
		}
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, 0)
		sig, err := i.tagSigner.SignPartial(tag)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("twoab: sign NR partial at L_0 (NRDirect): %w", err)
		}
		i.ownCommit = &Commit{
			ClusterID:    i.cfg.ClusterID,
			OperatorID:   i.ownOperatorID,
			Height:       i.cfg.Height,
			Side:         CommitSideNRDirect,
			L0Partial:    sig,
			LayerEntries: entries,
		}
		i.addToNoValuePool(0, i.ownOperatorID)
		i.addToNrTagPool(0, i.ownOperatorID, sig)
		i.phase2aFired = true
		// No afterStateDelta cascade here: NRDirect IS the commit, no
		// further emission to fire.
		return nil, nil, i.ownCommit, nil
	default:
		return nil, nil, nil, fmt.Errorf("twoab: unknown localValueState %d", state)
	}
}

// MaybeBuildAndBroadcastUpgrade evaluates the upgrade preconditions
// at L_0 and, if met, builds + records the upgrade KindValue.
//
// Preconditions (per spec §Trigger rules / Upgrade trigger):
//   - Op's only Phase-2 emission so far is KindNoValue (ownNoValueMsg
//     non-nil AND ownValueMsg nil AND ownCommit nil).
//   - Op now has V_0 retained at L_0.
//   - Host re-validates V_0 as valid at upgrade-emission time.
//
// On successful upgrade:
//   - ownValueMsg is set (alongside ownNoValueMsg — both are retained for
//     the NoValue→Value sequence on the wire).
//   - Receiver-side pool semantics (per §Receiver-side robustness): the
//     op is moved from noValuePool[0] to valuePool[0][V_0_root].
//   - The L_k>0 entries carried in the upgrade KindValue are identical
//     to those in the prior KindNoValue (per spec — the L_k>0 commitments
//     don't change on upgrade; only the L_0 emission kind changes).
//
// Returns:
//   - (*ValueMsg, nil) on successful upgrade (caller broadcasts).
//   - (nil, ErrUpgradeNotAvailable) if preconditions not met.
//   - (nil, err) on internal failure.
//
// Return-shape convention vs MaybeBuildAndBroadcastCommit: Upgrade uses
// an explicit `ErrUpgradeNotAvailable` sentinel for "no eligibility right
// now" because Upgrade has a SINGLE precondition cluster (op on KindNoValue
// path + has V_0 + host valid + not yet committed), and callers that
// explicitly invoke this path want to distinguish "tried but blocked" from
// "succeeded silently". Commit, by contrast, returns (nil, nil) for the
// no-trigger case because the NR-eligibility trigger is a passive
// observation of cluster pool state with no per-call distinguishable
// failure mode — the no-trigger state is the silent default (the slot's
// runner-level relay-submission deadline is the hard wall, not a per-
// call error). MaybeBuildAndBroadcastCommit fires on NR-eligibility
// alone: the equivocation trigger fires at Phase 2a only (as
// KindCommit-NRDirect), and there is no σ-eligibility trigger (the
// σ-side is terminal in KindValue). The cascade-driven afterStateDelta
// filters Upgrade's sentinel out via errors.Is so it doesn't pollute
// CascadeErrors().
//
// Idempotent across upgrade attempts: a second call after the upgrade
// has already fired returns the cached ownValueMsg + nil. After the op
// has emitted any Commit, the upgrade is unavailable — the authorized-
// sequence set is {NoValue→Value, NoValue→Commit-NR, Commit-NRDirect-
// alone}, so a KindValue following any KindCommit-{NR,NRDirect} would
// violate the σ-XOR-NR invariant (Rule 1 cross-signing).
func (i *Instance) MaybeBuildAndBroadcastUpgrade() (*ValueMsg, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return nil, ErrInstanceEnded
	}
	// Already upgraded — idempotent.
	if i.ownValueMsg != nil && i.ownNoValueMsg != nil {
		return i.ownValueMsg, nil
	}
	// Preconditions check.
	if i.ownNoValueMsg == nil {
		// Not on KindNoValue path.
		return nil, ErrUpgradeNotAvailable
	}
	if i.ownCommit != nil {
		// Post-commit upgrade is unauthorized.
		return nil, ErrUpgradeNotAvailable
	}
	const layer = 0
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]
	if len(retained) != 1 {
		// 0 retained → no V to upgrade on; ≥ 2 retained → leader
		// equivocation observed at L_0. There is no NR pivot here; the op
		// simply stays on the KindNoValue path and lets the NR fall-through
		// cascade proceed via the existing Phase 2b path.
		return nil, ErrUpgradeNotAvailable
	}
	v := retained[0].Bundle.Value
	valid, recorded := i.HostValidity(layer, v)
	if !recorded || !valid {
		return nil, ErrUpgradeNotAvailable
	}
	// Sign σ partial on V at L_0 and acquire σ-lock. The upgrade KindValue
	// IS the σ-side terminal emission — no follow-up σ commit. Sign-first
	// ordering matches MaybeFirePhase2a's Value-state branch (see that
	// branch for the sequencing rationale).
	root := ValueRoot(v)
	l0Partial, cached := i.ownPartials[layer]
	if !cached {
		p, err := i.signer.SignPartial(v)
		if err != nil {
			return nil, fmt.Errorf("twoab: sign σ at L_0 for upgrade: %w", err)
		}
		l0Partial = p
	}
	if err := i.transitionToSigma(layer, v); err != nil {
		return nil, fmt.Errorf("twoab: σ-lock at L_0 for upgrade: %w", err)
	}
	i.ownPartials[layer] = l0Partial
	i.addToSigmaPool(layer, root, i.ownOperatorID, l0Partial)
	// Build the upgrade KindValue. Per spec: identical wire shape to
	// the Phase-2a KindValue, including the K-1 LayerEntries carried
	// over from the prior KindNoValue. Forward leader σ-witnesses (L_0
	// plus each σ-side carried-over layer) so harvest-via-peer-KindValue
	// can propagate at every layer.
	upgradeEntries := cloneLayerEntries(i.ownNoValueMsg.LayerEntries)
	upgrade := &ValueMsg{
		ClusterID:    i.cfg.ClusterID,
		OperatorID:   i.ownOperatorID,
		Height:       i.cfg.Height,
		V:            append(Value{}, v...),
		ValueRoot:    root,
		Witnesses:    i.buildForwardedWitnesses(root, upgradeEntries),
		L0Partial:    append(Signature{}, l0Partial...),
		LayerEntries: upgradeEntries,
	}
	i.ownValueMsg = upgrade
	// Receiver-side pool semantics: move op from noValuePool[0] to
	// valuePool[0][V_root].
	i.removeFromNoValuePool(layer, i.ownOperatorID)
	i.addToValuePool(layer, root, i.ownOperatorID)
	return upgrade, nil
}

// OwnValueMsg returns the local operator's cached ValueMsg emission
// (either the Phase-2a fire-time emission or the Phase-2a-late upgrade),
// or (nil, false) if no ValueMsg has been emitted.
func (i *Instance) OwnValueMsg() (*ValueMsg, bool) {
	if i.ownValueMsg == nil {
		return nil, false
	}
	return i.ownValueMsg, true
}

// OwnNoValueMsg returns the local operator's cached NoValueMsg emission,
// or (nil, false) if no NoValueMsg has been emitted (op was Value-path
// or NRDirect-path at Phase 2a fire-time).
func (i *Instance) OwnNoValueMsg() (*NoValueMsg, bool) {
	if i.ownNoValueMsg == nil {
		return nil, false
	}
	return i.ownNoValueMsg, true
}

// OwnCommit returns the local operator's cached Commit emission (either
// the Phase-2a NRDirect emission or the Phase-2b NR commit), or
// (nil, false) if no Commit has been emitted yet.
func (i *Instance) OwnCommit() (*Commit, bool) {
	if i.ownCommit == nil {
		return nil, false
	}
	return i.ownCommit, true
}

// ObserveValueMsg records a peer's (or the local operator's own, after
// broadcast) Phase-2a ValueMsg. Per spec §Phase 2a / Pool aggregation:
//
//   - Bundles must pass structural validation (cluster id, slot, sender
//     in cluster, valid V + ValueRoot consistency, non-empty Witnesses
//     (with a Layer-0 entry) +
//     L0Partial, well-formed LayerEntries).
//   - First ValueMsg observed from op → recorded in peerValueMsg, pool
//     updates per inference rules. If a NoValueMsg was previously
//     observed from the same op, this is the upgrade — move op from
//     noValuePool[0] to valuePool[0][V_root]. The emitter's L0Partial is
//     verified and pooled into σ-pool[0][V_root][emitter].
//   - Identical re-broadcast (same content hash) → silent dedup.
//   - Distinct second ValueMsg (different V_0) → Rule 6a + Rule 3
//     evidence (cross-σ-V equivocation, when both L0Partials verify).
//   - Post-Commit ValueMsg observation: if the op had previously emitted
//     a KindCommit-NR / KindCommit-NRDirect, the resulting sequence is
//     unauthorized (σ-XOR-NR violation) → Rule 1 (cross-signing on
//     partial verify) + Rule 6a (sequence violation). The authorized set
//     is {NoValue→Value, NoValue→Commit-NR, Commit-NRDirect-alone}.
//
// Self-observation: the local op's own ValueMsg from MaybeFirePhase2a /
// MaybeBuildAndBroadcastUpgrade is already self-pool-updated in the
// build path; re-observing via this method (e.g., from a peer-echoed
// own broadcast) is a silent dedup no-op.
func (i *Instance) ObserveValueMsg(v *ValueMsg) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidateValueMsg(v, i.cfg); err != nil {
		return err
	}
	op := v.OperatorID
	// Self-observation dedup: own emissions are already pool-updated.
	if op == i.ownOperatorID {
		return nil
	}
	// Peer-reflood-V harvest: for each forwarded witness that verifies
	// against its layer-leader's pubKeyShare on the colocated V, treat the
	// observation as if we had received that leader's Phase-1 bundle
	// directly — synthesize the bundle and run it through the shared
	// retention path (which dedupes against directly-observed bundles and
	// idempotently seeds σ-pool[layer][V_root][leader]). This is the
	// recovery vector for HV1SelectiveDelivery, at every fall-through layer.
	//
	// On verify failure: silently discard. Do NOT fire Rule 5 — a
	// forwarded witness inside this envelope is signed-for-forwarding by the
	// emitter, not the leader; firing Rule 5 against the leader would
	// open a framing attack (byz emitter spoofs leader with random
	// bytes). The leader-signed-garbage attack stays covered in
	// ObservePhase1Bundle where the bundle's outer envelope binds the
	// bytes to the leader.
	//
	// The harvest runs ahead of the existing dedup/pool-update flow so
	// the afterStateDelta cascade at the end of ObserveValueMsg sees the
	// fresh retention state (enabling immediate upgrade for V-drop
	// receivers).
	//
	// Note on cascade-runs-twice: when a harvest establishes NEW retention
	// (priorRetained == 0), retainPhase1Bundle runs afterStateDelta
	// internally (from phase1.go:299 at end of the helper) before this
	// method's own afterStateDelta at the end of ObserveValueMsg. Both
	// runs are idempotent (per the cascade-helper contract —
	// MaybeBuildAndBroadcastUpgrade and MaybeBuildAndBroadcastCommit
	// return early on already-fired state). The first run sees
	// retention-fresh-but-no-peer-ValueMsg-pool-update (L0Partial pool
	// seed runs later in this method); the second run sees the fully-
	// updated state. Refactoring either run away would change observable
	// behavior (the upgrade can fire during the first run if the host
	// already validated the harvested V via an earlier path).
	//
	// In the dedup case (priorRetained ≥ 1 with V matching an existing
	// retention), retainPhase1Bundle returns early at the dedup loop
	// WITHOUT calling afterStateDelta — so only ObserveValueMsg's own
	// afterStateDelta runs. This is the common steady-state re-broadcast
	// path; the single run is correct because no retention-state delta
	// occurred.
	i.maybeHarvestPhase1BundleFromValueMsg(v)
	existing, hadValue := i.peerValueMsg[op]
	if hadValue {
		// Already have a ValueMsg from this op. Check content equality.
		if valueMsgContentHash(existing) == valueMsgContentHash(v) {
			// Identical re-broadcast — silent dedup.
			return nil
		}
		// Distinct second ValueMsg → Rule 6a (Phase-2 equivocation). Also
		// fires Rule 3 (cross-σ-V at L_0) when both messages carry σ
		// partials that verify on different V's — the σ partial is in
		// KindValue itself, so cross-σ-V detection lives here.
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      0,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					ValueA: existing,
					ValueB: deepCopyValueMsg(v),
				},
			})
		}
		if !bytes.Equal(existing.V, v.V) &&
			i.verifySigmaPartial(op, existing.V, existing.L0Partial) &&
			i.verifySigmaPartial(op, v.V, v.L0Partial) &&
			i.recordRule3(op, 0) {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossCommitEquivocation,
				OperatorID: op,
				Layer:      0,
				CrossCommitEquivocation: &CrossCommitEquivocationEvidence{
					ValueA:   append(Value{}, existing.V...),
					ValueB:   append(Value{}, v.V...),
					PartialA: append(Signature{}, existing.L0Partial...),
					PartialB: append(Signature{}, v.L0Partial...),
				},
			})
		}
		return nil
	}
	// Determine the authorized-sequence interpretation of this
	// observation relative to any prior peer emissions from op. Per spec
	// §Receiver ordering tolerance: a KindValue + KindNoValue pair received
	// in either order is interpreted as the upgrade. Any KindValue paired
	// with a KindCommit (NR or NRDirect) is slashable — KindValue's σ
	// partial conflicts with the Commit's NR partial under EKM σ-XOR-NR.
	// Reorder ambiguity (Commit-first vs KindValue-first) doesn't change
	// the outcome; the cross-signing / sequence-violation evidence fires in
	// either order.
	hadNoValue := i.peerNoValueMsg[op] != nil
	hadCommit := i.peerCommit[op]
	const layer = 0
	if hadCommit != nil {
		// Any prior Commit means the op already chose NR-side at L_0. This
		// KindValue (σ-side) is a cross-σ-NR violation. Rule 1 fires on
		// cryptographic verify; Rule 6a fires on the sequence violation
		// regardless.
		if i.verifySigmaPartial(op, v.V, v.L0Partial) &&
			i.verifyNRTagPartial(op, layer, hadCommit.L0Partial) &&
			i.recordRule1(op, layer) {
			i.recordEvidence(Evidence{
				Rule:       EvidenceCrossSigning,
				OperatorID: op,
				Layer:      layer,
				CrossSigning: &CrossSigningEvidence{
					SigmaPartial: append(Signature{}, v.L0Partial...),
					SigmaValue:   append(Value{}, v.V...),
					NRPartial:    append(Signature{}, hadCommit.L0Partial...),
				},
			})
		}
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      layer,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					CommitA: hadCommit,
					ValueB:  deepCopyValueMsg(v),
				},
			})
		}
		i.peerValueMsg[op] = deepCopyValueMsg(v)
		// L0Partial pool / L_k entries handled below in the unified
		// pool-update block (fall through after rule firing).
		i.addToValuePool(layer, v.ValueRoot, op)
		i.verifyAndPoolL0Partial(op, v)
		i.processObservedLayerEntries(op, v.LayerEntries)
		i.afterStateDelta()
		return nil
	}
	// No prior Commit. Fresh ValueMsg or upgrade (prior NoValueMsg).
	if hadNoValue {
		// Upgrade: move op from noValuePool[0] to valuePool[0].
		// Per spec §Phase 2a-late upgrade, the L_k>0 entries MUST be
		// identical to those in the prior NoValueMsg. A byz emitting an
		// upgrade with mismatched L_k entries would attempt to inject
		// inconsistent pool memberships at L_k>0 — slashable per Rule 6a.
		priorNoValue := i.peerNoValueMsg[op]
		if !layerEntriesEqual(priorNoValue.LayerEntries, v.LayerEntries) {
			if i.recordRule6a(op) {
				i.recordEvidence(Evidence{
					Rule:       EvidencePhase2Equivocation,
					OperatorID: op,
					Layer:      layer,
					Phase2Equivocation: &Phase2EquivocationEvidence{
						NoValueA: priorNoValue,
						ValueB:   deepCopyValueMsg(v),
					},
				})
			}
			// Don't process the upgrade's mismatched L_k entries — the
			// prior NoValueMsg's contributions stand. L_0 move still
			// happens (the upgrade itself is a valid L_0 claim).
		}
		i.peerValueMsg[op] = deepCopyValueMsg(v)
		i.removeFromNoValuePool(layer, op)
		i.addToValuePool(layer, v.ValueRoot, op)
	} else {
		i.peerValueMsg[op] = deepCopyValueMsg(v)
		i.addToValuePool(layer, v.ValueRoot, op)
		// L_k>0 entries contribute to deeper-layer pools per inference rules.
		i.processObservedLayerEntries(op, v.LayerEntries)
	}
	// Verify+pool the emitter's L0Partial into σ-pool. Done AFTER the main
	// pool-update flow so retention/sequence-rule evidence fires first; the
	// σ-pool seed is the cryptographic "we have an actual partial" step.
	i.verifyAndPoolL0Partial(op, v)
	i.afterStateDelta()
	return nil
}

// verifyAndPoolL0Partial verifies the emitter's L0Partial against their
// pubKeyShare on v.V and pools into σ-pool[L_0][V_root][emitter] on
// success. On verify failure fires Rule 5 (fake plaintext σ at L_0)
// against the emitter — the L0Partial is the emitter's OWN signing
// artifact (verifiable against pubKeyShares[v.OperatorID]), so a failed
// verify is unambiguous emitter misbehavior (no framing-attack symmetry
// with a forwarded witness, which is signed-for-forwarding by the emitter
// rather than self-attributed).
func (i *Instance) verifyAndPoolL0Partial(emitter OperatorID, v *ValueMsg) {
	if len(v.L0Partial) == 0 {
		// Defensive: ValidateValueMsg requires a non-empty L0Partial, so
		// this branch shouldn't fire on validated input. Treat as silent
		// skip — no Rule 5 (no claim to verify against).
		return
	}
	const layer = 0
	if i.verifySigmaPartial(emitter, v.V, v.L0Partial) {
		i.addToSigmaPool(layer, ValueRoot(v.V), emitter, v.L0Partial)
		// Cross-source Rule 3: the emitter may already hold a σ partial on a
		// different V at L_0 (its own Phase-1 LeaderSigma as the layer leader, or
		// a harvested witness). Re-check now, in any arrival order.
		i.maybeFireCrossSigmaV(emitter, v.V)
		return
	}
	if i.recordRule5(emitter, layer) {
		i.recordEvidence(Evidence{
			Rule:       EvidenceFakePlaintextSigma,
			OperatorID: emitter,
			Layer:      layer,
			FakePlaintextSigma: &FakePlaintextSigmaEvidence{
				OnionPartial:        append(Signature{}, v.L0Partial...),
				OnionValue:          append(Value{}, v.V...),
				RetainedValueHashes: i.retainedValueHashes(0),
			},
		})
	}
}

// ObserveNoValueMsg records a peer's Phase-2a NoValueMsg. Per spec
// §Phase 2a / Pool aggregation:
//
//   - Validation as for ObserveValueMsg.
//   - First NoValueMsg → recorded in peerNoValueMsg, op added to
//     noValuePool[0]. L_k>0 entries contribute to deeper-layer pools.
//   - Identical re-broadcast → silent dedup.
//   - Distinct second NoValueMsg (different LayerEntries content) →
//     Rule 6a evidence. (Two distinct NoValueMsgs from same op is
//     unusual but possible if op equivocates on L_k>0 entry choices.)
//   - Post-Commit NoValueMsg → Rule 6a evidence (unauthorized sequence).
//
// Self-observation: silent dedup (own emissions are self-pool-updated).
func (i *Instance) ObserveNoValueMsg(nv *NoValueMsg) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if i.ended {
		return ErrInstanceEnded
	}
	if err := ValidateNoValueMsg(nv, i.cfg); err != nil {
		return err
	}
	op := nv.OperatorID
	if op == i.ownOperatorID {
		return nil
	}
	existing, hadNoValue := i.peerNoValueMsg[op]
	if hadNoValue {
		if noValueMsgContentHash(existing) == noValueMsgContentHash(nv) {
			return nil // identical re-broadcast
		}
		// Distinct second NoValueMsg — Rule 6a equivocation.
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      0,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					NoValueA: existing,
					NoValueB: deepCopyNoValueMsg(nv),
				},
			})
		}
		return nil
	}
	// Per spec §Receiver ordering tolerance: a `KindValue` + `KindNoValue`
	// pair in either order is the upgrade (op originally emitted NoValue at
	// Phase 2a, then upgraded to Value). NOT slashable. A `KindNoValue` +
	// `KindCommit-NR` pair is the NoValue→Commit-NR fall-through. NOT
	// slashable. Only the post-NRDirect case (KindNoValue arriving after
	// KindCommit-NRDirect, which is a sole emission) is unambiguously
	// slashable here.
	hadValue := i.peerValueMsg[op]
	hadCommit := i.peerCommit[op]
	const layer = 0
	if hadCommit != nil && hadCommit.Side == CommitSideNRDirect {
		// Post-NRDirect: any further emission is slashable (NRDirect is a
		// sole emission).
		if i.recordRule6a(op) {
			i.recordEvidence(Evidence{
				Rule:       EvidencePhase2Equivocation,
				OperatorID: op,
				Layer:      layer,
				Phase2Equivocation: &Phase2EquivocationEvidence{
					CommitA:  hadCommit,
					NoValueB: deepCopyNoValueMsg(nv),
				},
			})
		}
		i.peerNoValueMsg[op] = deepCopyNoValueMsg(nv)
		return nil
	}
	if hadValue != nil {
		// Upgrade reorder: op emitted KindNoValue at Phase 2a then
		// upgraded to KindValue (already observed). The current NoValueMsg
		// is the original Phase-2a emission arriving late. NOT slashable
		// at the pair level (per §Receiver ordering tolerance).
		//
		// However: per spec §Phase 2a-late upgrade, the L_k>0 entries MUST
		// be identical across the pair. If they differ, the byz crafted
		// inconsistent entries — Rule 6a fires, and we DON'T re-process
		// the NoValueMsg's L_k entries (the upgrade's contributions stand;
		// the byz's late-arriving mismatch is discarded to prevent
		// cross-pool injection at L_k>0).
		i.peerNoValueMsg[op] = deepCopyNoValueMsg(nv)
		if !layerEntriesEqual(hadValue.LayerEntries, nv.LayerEntries) {
			if i.recordRule6a(op) {
				i.recordEvidence(Evidence{
					Rule:       EvidencePhase2Equivocation,
					OperatorID: op,
					Layer:      layer,
					Phase2Equivocation: &Phase2EquivocationEvidence{
						ValueA:   hadValue,
						NoValueB: deepCopyNoValueMsg(nv),
					},
				})
			}
			// Don't re-process — the upgrade's L_k entries already
			// populated the pools when ObserveValueMsg ran.
		} else {
			// L_k>0 entries from this NoValueMsg are identical to those
			// in the upgrade KindValue — re-process defensively (pool
			// updates are idempotent).
			i.processObservedLayerEntries(op, nv.LayerEntries)
		}
		i.afterStateDelta()
		return nil
	}
	// Fresh NoValueMsg, OR Commit-NR arrived earlier (reorder).
	i.peerNoValueMsg[op] = deepCopyNoValueMsg(nv)
	if hadCommit == nil {
		// No prior commit — op is on NoValue path; add to noValuePool.
		i.addToNoValuePool(layer, op)
	}
	// If hadCommit != nil with Side == NR: reordered NoValue→Commit-NR
	// fall-through arrived out of order. The Commit has already moved the
	// op into nrTagPool; the late-arriving NoValueMsg doesn't re-introduce
	// a noValuePool entry. L_k>0 entries are processed either way
	// (idempotent).
	i.processObservedLayerEntries(op, nv.LayerEntries)
	i.afterStateDelta()
	return nil
}

// processObservedLayerEntries updates the claim and threshold pools for
// the L_k>0 LayerEntries observed from `op`. Per §Pool aggregation rules
// / L_k>0 entry contributions:
//
//   - SigmaChained entry at layer k → add op to valuePool[k][V_root].
//     The actual σ partial is in the chained-encrypted Payload; the
//     plaintext partial gets added to sigmaPool[k] only later, during
//     Resolve()'s chain-decryption walk once enough nr_tag partials at
//     prior layers have aggregated to unlock the layer.
//   - NRPlaintext entry at layer k → if the partial verifies against op's
//     nr_tag share, add op to noValuePool[k] AND to nrTagPool[k] (the
//     partial is plaintext on the wire, immediately usable for Phase 3
//     unlock-chain aggregation). Without verification, a byz could spam
//     garbage partials and poison the nr_tag_k threshold pool.
//   - Empty entry → no pool update.
func (i *Instance) processObservedLayerEntries(op OperatorID, entries []LayerEntry) {
	for _, e := range entries {
		switch e.Kind {
		case LayerEntrySigmaChained:
			i.addToValuePool(e.Layer, ValueRoot(e.V), op)
		case LayerEntryNRPlaintext:
			if i.verifyNRTagPartial(op, e.Layer, Signature(e.Payload)) {
				i.addToNoValuePool(e.Layer, op)
				i.addToNrTagPool(e.Layer, op, Signature(e.Payload))
			}
			// On verify failure: skip the entry. The byz's bogus NR
			// claim at this layer doesn't poison the threshold pool.
			// The rest of the message's entries continue to be processed.
		case LayerEntryEmpty:
			// no-op
		}
	}
}

// maybeHarvestPhase1BundleFromValueMsg implements the peer-reflood-V
// harvest. For each forwarded witness in the peer's
// KindValue (Witnesses[]), if it verifies against its layer-leader's
// pubKeyShare on the colocated V (v.V at L_0, the SigmaChained entry at
// k>0), synthesize a Phase-1 bundle (as if that leader's bundle had reached
// us directly) and feed it to the shared retention path. The retention
// helper handles dedup against any already-retained bundle for that
// (layer, leader) — so direct-then-harvest and harvest-then-direct
// converge — and seeds σ-pool[layer][V_root] with the leader's partial.
// See harvestOneWitness for the per-witness logic.
//
// On successful harvest that establishes NEW retention (priorRetained == 0
// at L_0 for this leader), the Instance enqueues a ValidationRequest on
// wantsHostValidationCh so the runner can ask the host to validate the
// harvested V. The upgrade trigger fires off the host's reply via
// ApplyHostValidity's afterStateDelta cascade. The channel-based hand-off
// decouples the host-call timing from the harvest moment — runners with
// scheduling policies can drain at their own cadence without changing
// protocol semantics.
//
// Direct-observation retention does NOT enqueue: ObservePhase1Bundle's
// caller (`evtPhase1Arrival`) already runs ApplyHostValidity inline.
//
// observedOffset for the synthetic bundle is set to 0 — there is no real
// "leader-bundle arrival time" to record; the field is diagnostic-only
// (no protocol branches on it) and 0 is the documented sentinel for
// synthetic-from-peer-harvest retention. If timing-based diagnostics
// become load-bearing later, lift this to an `observedOffset` parameter
// on ObserveValueMsg.
//
// Verify failure: silent discard, no Rule 5, no request enqueue (see
// ObserveValueMsg's docstring for the framing-attack rationale).
func (i *Instance) maybeHarvestPhase1BundleFromValueMsg(v *ValueMsg) {
	// Iterate the forwarded witnesses (L_0 plus each σ-side deeper layer).
	for _, w := range v.Witnesses {
		i.harvestOneWitness(v, w)
	}
}

// harvestOneWitness verifies + retains a single forwarded LayerWitness from
// a peer KindValue (at L_0 and at deeper layers). The V bytes needed
// to BLS-verify the witness ride alongside in the SAME KindValue (root +
// σ-colocation): v.V at Layer 0, the SigmaChained LayerEntry's V at k>0. On
// verify failure / missing colocated V: silent discard (anti-framing — the
// forwarded witness isn't envelope-attributed to the leader, so a fake one
// must NOT fire Rule 5 against the leader; see ObserveValueMsg's docstring).
func (i *Instance) harvestOneWitness(v *ValueMsg, w LayerWitness) {
	if len(w.Witness) == 0 {
		return
	}
	if w.Layer < 0 || w.Layer >= i.cfg.K() {
		return
	}
	// Locate the colocated V whose root the witness references.
	var vK Value
	if w.Layer == 0 {
		if w.ValueRoot != v.ValueRoot {
			return // the L_0 witness must reference this KindValue's own V
		}
		vK = v.V
	} else {
		for _, e := range v.LayerEntries {
			if e.Layer == w.Layer && e.Kind == LayerEntrySigmaChained && ValueRoot(e.V) == w.ValueRoot {
				vK = e.V
				break
			}
		}
		if len(vK) == 0 {
			return // no colocated V → can't verify/harvest this witness
		}
	}
	leaderID := i.cfg.Layers[w.Layer].Leader
	if !i.verifyWitnessCached(w.Layer, vK, w.Witness, leaderID) {
		return
	}
	// Snapshot retention BEFORE harvest so we can detect first-time
	// retention establishment (true V-drop receiver). Only true V-drops at
	// L_0 need a host-validation request; receivers with prior retention
	// have already been queried by evtPhase1Arrival.
	priorRetained := len(i.retainedBundles[w.Layer][leaderID])
	synth := &Phase1Bundle{
		ClusterID:   i.cfg.ClusterID,
		OperatorID:  leaderID,
		Height:      i.cfg.Height,
		Layer:       w.Layer,
		Value:       append(Value{}, vK...),
		LeaderSigma: append(Signature{}, w.Witness...),
	}
	// Defense-in-depth: re-validate the synthesized bundle structurally
	// before retention. Today this is redundant (synth is built from
	// already-validated fields) but a future tightening of
	// ValidatePhase1Bundle would otherwise silently skip the harvest path.
	if err := ValidatePhase1Bundle(synth, i.cfg); err != nil {
		return
	}
	i.retainPhase1Bundle(synth, 0 /* observedOffset sentinel */, true /* witnessPreVerified */)
	// Host-validation request only for L_0 first-retention (priorRetained
	// == 0). It drives the upgrade — the V-drop recovery vector — which
	// is an L_0-only mechanism. A harvested V_k>0 has no upgrade path: the
	// op has already fired Phase-2a with its L_k commitment fixed, so the
	// harvest only seeds σ-pool[k] for Resolve's reconstruction; no host
	// query is needed. For priorRetained ≥ 1 the receiver already has
	// retention for this leader and the host has been queried (or is
	// in-flight); re-enqueuing would duplicate work.
	//
	// Note: when priorRetained == 0, retainPhase1Bundle unconditionally
	// adds (the synth's V is new — no dedup target, no ≥ 2 cap hit), so the
	// post-call retention length is always ≥ 1; a `len(...) == 0` check
	// would be structurally unreachable.
	if w.Layer != 0 || priorRetained != 0 {
		return
	}
	// First-time L_0 retention via harvest. Push validation request — the
	// runner's host hook is the source of truth for V-validity, and the
	// channel-based hand-off lets production runners apply their own
	// scheduling policy.
	//
	// We don't gate the push on a "NR-fall-through still feasible"
	// heuristic. Such a gate would correctly defer in scenarios like
	// LateLeaderBroadcast (where peers are about to emit KindNoValue and
	// the cluster can fall through to L_1) but would race-fail in
	// scenarios like HV1SelectiveDelivery: at the harvest moment, the
	// receiver may not have observed enough peer Phase-2a state to know
	// whether NR-fall-through is feasible cluster-wide. The DES processes
	// per-peer arrivals in scheduling order; in HV1SelectiveDelivery, the
	// byz leader's KindValue arrives at the V-drop before the V-recipient's
	// — at which point only ONE peer's Phase-2a is observed, and a
	// no-NR-reachable heuristic would optimistically defer (false
	// negative). The resulting MISS in HV1Selective is the worst possible
	// outcome — the cluster's only L_0 recovery vector silently doesn't
	// fire.
	//
	// Trade-off accepted: under LateLeaderBroadcast, the cluster prefers
	// σ-at-L_0 (via the harvest-driven upgrade) over NR-fall-through to
	// L_1. When the leader's broadcast is sufficiently late the σ-cascade
	// doesn't fit in the slot budget and the cluster MISSes (a design
	// without the leader-witness head-start would instead fall through and
	// succeed at L_1). This trade-off is mechanical and scenario-specific.
	i.requestHostValidation(0, vK)
}

// verifyWitnessCached wraps verifySigmaPartial with a memo on
// (layer, V_root) — every peer forwards the same leader witness in its
// KindValue, and a re-broadcast cluster would naively re-verify N times
// per slot. The cache short-circuits subsequent verifies once the
// (layer, V_root) is known-good. Cache hits return true without invoking
// the BLS primitive; cache misses run verify and cache on success. The
// cache is layer-indexed (verifiedWitnesses[layer][V_root]) so per-layer
// forwarded witnesses each get their own dedup bucket.
//
// Cache is positive-only: a verify failure does NOT cache. A byz emitter
// presenting bogus bytes on the same V_root each time will still pay
// re-verify cost, but they're punished by other mechanisms (Rule 6a on
// cross-V; gossipsub scoring). Avoiding negative-caching keeps the door
// open for a subsequent valid witness on the same V to land — for
// instance, if the first observed witness was corrupted on the wire
// (truncation, malicious mutation by a relay) and a second arrival
// carries the correct bytes.
//
// Per spec (verify-cost dedup); mirrors OBFT's witnessedLeaderSigma.
func (i *Instance) verifyWitnessCached(layer int, v Value, witness Signature, leaderID OperatorID) bool {
	root := ValueRoot(v)
	if bucket := i.verifiedWitnesses[layer]; bucket != nil {
		if bucket[root] {
			return true
		}
	}
	if !i.verifySigmaPartial(leaderID, v, witness) {
		return false
	}
	bucket := i.verifiedWitnesses[layer]
	if bucket == nil {
		bucket = make(map[[32]byte]bool)
		i.verifiedWitnesses[layer] = bucket
	}
	bucket[root] = true
	return true
}

// deepCopyValueMsg returns an independent copy of v.
func deepCopyValueMsg(v *ValueMsg) *ValueMsg {
	if v == nil {
		return nil
	}
	out := *v
	out.V = append(Value{}, v.V...)
	out.Witnesses = cloneLayerWitnesses(v.Witnesses)
	out.L0Partial = append(Signature{}, v.L0Partial...)
	out.LayerEntries = cloneLayerEntries(v.LayerEntries)
	return &out
}

// deepCopyNoValueMsg returns an independent copy of nv.
func deepCopyNoValueMsg(nv *NoValueMsg) *NoValueMsg {
	if nv == nil {
		return nil
	}
	out := *nv
	out.LayerEntries = cloneLayerEntries(nv.LayerEntries)
	return &out
}

// cloneLayerWitnesses returns an independent deep copy of witnesses.
func cloneLayerWitnesses(ws []LayerWitness) []LayerWitness {
	if ws == nil {
		return nil
	}
	out := make([]LayerWitness, len(ws))
	for i, w := range ws {
		out[i] = LayerWitness{
			Layer:     w.Layer,
			ValueRoot: w.ValueRoot,
			Witness:   append(Signature{}, w.Witness...),
		}
	}
	return out
}

// cloneLayerEntries returns an independent deep copy of entries.
func cloneLayerEntries(entries []LayerEntry) []LayerEntry {
	if entries == nil {
		return nil
	}
	out := make([]LayerEntry, len(entries))
	for i, e := range entries {
		out[i] = LayerEntry{
			Layer:   e.Layer,
			Kind:    e.Kind,
			V:       append(Value{}, e.V...),
			Payload: append([]byte{}, e.Payload...),
		}
	}
	return out
}
