package twoab

import (
	"fmt"
)

// ApplyHostValidity records the host application's valid / not-valid
// verdict on the given V at the given layer. Per spec §Phase 2a, the
// host is consulted at:
//
//   - Phase-2a fire-time: to determine whether the op emits KindValue
//     (host says valid AND op has V) or KindNoValue (otherwise).
//   - Each commit emission time (host re-check): to route the σ-eligibility
//     trigger to KindCommit-Signed vs KindCommit-NR. The host MAY flip
//     its verdict between Phase 2a and commit time (mid-slot re-org); the
//     re-check at commit time is what enables A3 (host-flip pivot).
//
// Idempotent for the same (layer, V, valid) triple. Calling with a
// different `valid` value for the same (layer, V) overwrites — the
// most recent host verdict wins, consistent with the spec's intent
// that host stabilization narrows the divergence window over time.
//
// On L_0 host-validity updates, the Instance runs the per-tick
// processing cascade so a host-flip-to-valid can trigger the A1 upgrade
// path (if the op is on KindNoValue path AND now has V_0 + host says
// valid) and the σ-eligibility trigger gets a chance to fire on the
// resulting state.
//
// Returns an error if `layer` is out of [0, K) or `value` is empty.
func (i *Instance) ApplyHostValidity(layer int, value Value, valid bool) error {
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
	i.afterStateDelta()
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

// buildLayerEntry constructs a single LayerEntry at layer k > 0.
func (i *Instance) buildLayerEntry(k int) (LayerEntry, error) {
	if k <= 0 || k >= i.cfg.K() {
		return LayerEntry{}, fmt.Errorf("twoab: %w: layer %d outside (0, %d)",
			ErrLayerOutOfRange, k, i.cfg.K())
	}
	leaderID := i.cfg.Layers[k].Leader
	retained := i.retainedBundles[k][leaderID]

	// Equivocation observed at L_k → NR-side at this layer.
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
// Idempotent: subsequent calls return (nil, nil) silently (the cached
// emission is whatever was set on first call). Callers should examine
// OwnValueMsg / OwnNoValueMsg / OwnCommit to retrieve the emission.
//
// Returns a triple {ValueMsg, NoValueMsg, Commit} — exactly one is non-nil
// (matching the local value state). Returns an error if Phase 2a build
// fails (e.g., signer error during LayerEntry construction).
func (i *Instance) MaybeFirePhase2a() (*ValueMsg, *NoValueMsg, *Commit, error) {
	if i == nil {
		return nil, nil, nil, fmt.Errorf("twoab: nil instance")
	}
	if i.phase2aFired {
		return i.ownValueMsg, i.ownNoValueMsg, i.ownCommit, nil
	}
	state := i.computeLocalValueState()
	entries, err := i.buildLayerEntries()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("twoab: build LayerEntries: %w", err)
	}
	i.phase2aFired = true
	switch state {
	case localValueStateValue:
		const layer = 0
		retained := i.retainedBundles[layer][i.cfg.Layers[layer].Leader]
		v := retained[0].Bundle.Value
		root := ValueRoot(v)
		i.ownValueMsg = &ValueMsg{
			ClusterID:    i.cfg.ClusterID,
			OperatorID:   i.ownOperatorID,
			Height:       i.cfg.Height,
			V:            append(Value{}, v...),
			ValueRoot:    root,
			LayerEntries: entries,
		}
		i.addToValuePool(layer, root, i.ownOperatorID)
		// The σ partial at L_0 will be signed at Commit-Signed emission
		// time, not here — ValueMsg is op-id-signed coordination only.
		// Phase 2a fire is a state delta — run the per-tick cascade so
		// commit triggers can fire immediately if cluster σ-eligibility
		// is already satisfied (e.g., peer Phase-2a emissions arrived
		// before our fire-instant).
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
		i.afterStateDelta()
		return nil, i.ownNoValueMsg, nil, nil
	case localValueStateNRDirect:
		// Phase-2a NR-direct: equivocation observed at L_0. Emit a
		// Commit with Side=NRDirect that bundles the L_0 nr_tag_0
		// partial + the K-1 LayerEntries (since the op skips KindValue
		// / KindNoValue entirely per A8).
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
		// No afterStateDelta cascade here: NRDirect IS the commit, no
		// further emission to fire.
		return nil, nil, i.ownCommit, nil
	default:
		return nil, nil, nil, fmt.Errorf("twoab: unknown localValueState %d", state)
	}
}

// MaybeBuildAndBroadcastUpgrade evaluates the A1 upgrade preconditions
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
//     the A1 sequence on the wire).
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
// Idempotent across upgrade attempts: a second call after the upgrade
// has already fired returns the cached ownValueMsg + nil. After the op
// has emitted any Commit, the upgrade is no longer available (post-
// commit upgrade is the slashable sequence A1+A5+A1).
func (i *Instance) MaybeBuildAndBroadcastUpgrade() (*ValueMsg, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
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
		// 0 retained → no V to upgrade on; ≥ 2 retained → equivocation
		// observed (would route to A4 NR pivot, not upgrade).
		return nil, ErrUpgradeNotAvailable
	}
	v := retained[0].Bundle.Value
	valid, recorded := i.HostValidity(layer, v)
	if !recorded || !valid {
		return nil, ErrUpgradeNotAvailable
	}
	// Build the upgrade KindValue. Per spec: identical wire shape to
	// the Phase-2a KindValue, including the K-1 LayerEntries carried
	// over from the prior KindNoValue.
	root := ValueRoot(v)
	upgrade := &ValueMsg{
		ClusterID:    i.cfg.ClusterID,
		OperatorID:   i.ownOperatorID,
		Height:       i.cfg.Height,
		V:            append(Value{}, v...),
		ValueRoot:    root,
		LayerEntries: cloneLayerEntries(i.ownNoValueMsg.LayerEntries),
	}
	i.ownValueMsg = upgrade
	// Receiver-side pool semantics: move op from noValuePool[0] to
	// valuePool[0][V_root].
	i.removeFromNoValuePool(layer, i.ownOperatorID)
	i.addToValuePool(layer, root, i.ownOperatorID)
	return upgrade, nil
}

// OwnValueMsg returns the local operator's cached ValueMsg emission
// (either the Phase-2a fire-time emission or the Phase-2a-late A1
// upgrade), or (nil, false) if no ValueMsg has been emitted.
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
// the Phase-2a NRDirect emission or the Phase-2b Signed/NR commit), or
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
//     in cluster, valid V + ValueRoot consistency, well-formed
//     LayerEntries).
//   - First ValueMsg observed from op → recorded in peerValueMsg, pool
//     updates per inference rules. If a NoValueMsg was previously
//     observed from the same op, this is the A1 upgrade — move op from
//     noValuePool[0] to valuePool[0][V_root].
//   - Identical re-broadcast (same content hash) → silent dedup.
//   - Distinct second ValueMsg (different V_0) → Rule 6a + Rule 3
//     evidence (cross-σ-V equivocation).
//   - Post-Commit ValueMsg observation: if the op had previously emitted
//     a KindCommit (Signed or NR), the resulting sequence is
//     unauthorized (A5+A1 / A2+A1 / etc.) → Rule 6a evidence.
//
// Self-observation: the local op's own ValueMsg from MaybeFirePhase2a /
// MaybeBuildAndBroadcastUpgrade is already self-pool-updated in the
// build path; re-observing via this method (e.g., from a peer-echoed
// own broadcast) is a silent dedup no-op.
func (i *Instance) ObserveValueMsg(v *ValueMsg) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidateValueMsg(v, i.cfg); err != nil {
		return err
	}
	op := v.OperatorID
	// Self-observation dedup: own emissions are already pool-updated.
	if op == i.ownOperatorID {
		return nil
	}
	existing, hadValue := i.peerValueMsg[op]
	if hadValue {
		// Already have a ValueMsg from this op. Check content equality.
		if valueMsgContentHash(existing) == valueMsgContentHash(v) {
			// Identical re-broadcast — silent dedup.
			return nil
		}
		// Distinct second ValueMsg → Rule 6a Phase-2 equivocation.
		// Also Rule 3 cross-σ-V (different V_0).
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
		return nil
	}
	// Determine the authorized-sequence interpretation of this
	// observation relative to any prior peer emissions from op. Per spec
	// §Receiver ordering tolerance (plan line 593): a `KindValue` +
	// `KindNoValue` pair received in either order is interpreted as A1
	// (upgrade). A `KindValue` + `KindCommit-{Signed,NR}` pair is
	// interpreted as A2/A3/A4 (authorized). Only the post-NRDirect case
	// is unambiguously slashable here (A8 is sole-emission per spec).
	hadNoValue := i.peerNoValueMsg[op] != nil
	hadCommit := i.peerCommit[op]
	const layer = 0
	if hadCommit != nil && hadCommit.Side == CommitSideNRDirect {
		// Post-NRDirect observation of any other emission from same op
		// is unambiguously slashable (A8 forbids further emissions).
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
		return nil
	}
	// Fresh ValueMsg, A1 upgrade (prior NoValueMsg), or reorder of a
	// Commit-{Signed,NR} sequence — all authorized. Pool-update.
	i.peerValueMsg[op] = deepCopyValueMsg(v)
	if hadNoValue {
		// A1 upgrade: move op from noValuePool[0] to valuePool[0].
		i.removeFromNoValuePool(layer, op)
	}
	i.addToValuePool(layer, v.ValueRoot, op)
	// L_k>0 entries contribute to deeper-layer pools per inference rules.
	i.processObservedLayerEntries(op, v.LayerEntries)
	i.afterStateDelta()
	return nil
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
	// pair in either order is interpreted as A1 (upgrade — op originally
	// emitted NoValue at Phase 2a, then upgraded to Value). NOT slashable.
	// A `KindNoValue` + `KindCommit-{Signed,NR}` pair is interpreted as
	// A5/A6/A7 (authorized). Only the post-NRDirect case is unambiguously
	// slashable here.
	hadValue := i.peerValueMsg[op]
	hadCommit := i.peerCommit[op]
	const layer = 0
	if hadCommit != nil && hadCommit.Side == CommitSideNRDirect {
		// Post-NRDirect: any further emission is slashable (A8 sole-emission).
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
		// A1 upgrade reorder: op emitted KindNoValue at Phase 2a then
		// upgraded to KindValue (already observed). The current NoValueMsg
		// is the original Phase-2a emission arriving late. NOT slashable.
		// Pool semantics: op is in valuePool[0][V_root] (from the prior
		// KindValue); do NOT add to noValuePool — the upgrade superseded
		// the NoValue contribution.
		i.peerNoValueMsg[op] = deepCopyNoValueMsg(nv)
		// L_k>0 entries from this NoValueMsg are identical to those in
		// the upgrade KindValue (per spec — upgrade carries over the
		// L_k>0 entries). Process them defensively — pool updates are
		// idempotent so this is a no-op if the upgrade was already processed.
		i.processObservedLayerEntries(op, nv.LayerEntries)
		i.afterStateDelta()
		return nil
	}
	// Fresh NoValueMsg, OR Commit-{Signed,NR} arrived earlier (reorder).
	i.peerNoValueMsg[op] = deepCopyNoValueMsg(nv)
	if hadCommit == nil {
		// No prior commit — op is on NoValue path; add to noValuePool.
		i.addToNoValuePool(layer, op)
	}
	// If hadCommit != nil with Side in {Signed, NR}: reordered A2/A5/A6/A7.
	// The Commit has already moved the op into valuePool or sigmaPool /
	// nrTagPool as appropriate; the late-arriving NoValueMsg doesn't
	// re-introduce a noValuePool entry. L_k>0 entries are processed
	// either way (idempotent).
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
//   - NRPlaintext entry at layer k → add op to noValuePool[k] AND to
//     nrTagPool[k] (the partial is plaintext on the wire, immediately
//     usable for Phase 3 unlock-chain aggregation).
//   - Empty entry → no pool update.
func (i *Instance) processObservedLayerEntries(op OperatorID, entries []LayerEntry) {
	for _, e := range entries {
		switch e.Kind {
		case LayerEntrySigmaChained:
			i.addToValuePool(e.Layer, ValueRoot(e.V), op)
		case LayerEntryNRPlaintext:
			i.addToNoValuePool(e.Layer, op)
			i.addToNrTagPool(e.Layer, op, Signature(e.Payload))
		case LayerEntryEmpty:
			// no-op
		}
	}
}

// deepCopyValueMsg returns an independent copy of v.
func deepCopyValueMsg(v *ValueMsg) *ValueMsg {
	if v == nil {
		return nil
	}
	out := *v
	out.V = append(Value{}, v.V...)
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

