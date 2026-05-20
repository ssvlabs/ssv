package twoab

import "fmt"

// ApplyHostValidity records the host application's valid / not-valid
// verdict on the given V at the given layer. Per spec §Phase 2a, the
// host is consulted at Phase-2a verdict-broadcast time (separately from
// any Phase-1 host check the application may have done); this method
// is the protocol-layer entry point for that consultation.
//
// Idempotent for the same (layer, V, valid) triple. Calling with a
// different `valid` value for the same (layer, V) overwrites — the
// most recent host verdict wins, consistent with the spec's intent
// that host stabilization narrows the divergence window over time.
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
	if layer == 0 {
		i.maybeSignalL0VerdictReady()
	}
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

// ComputeLocalVerdict applies the 4-case rule from spec §Phase 2a to
// determine this operator's verdict at the given layer:
//
//   - retained ≥ 2 distinct V's (equivocation observed)          → NR
//   - retained 1 V, host says valid, regular retention           → σV
//   - retained 1 V, host says not-valid, regular retention       → NV
//   - retained 0 V's (or only auth-only retention)               → NR
//
// Auth-only-retained bundles (first-observed past T_accept_max) count
// toward equivocation detection but do NOT enable σV/NV verdicts. Per
// spec §Phase 1: "Auth-only retention does not allow the operator to
// issue a KindVerdict based on this bundle."
//
// Returns (kind, value_root, error). The value_root is non-zero only
// for σV verdicts. The error is non-nil when the local operator needs
// host validity for a single retained V but ApplyHostValidity hasn't
// been called for that V — the runner must call ApplyHostValidity at
// Phase-2a verdict-broadcast time before BuildVerdict.
func (i *Instance) ComputeLocalVerdict(layer int) (VerdictKind, [32]byte, error) {
	if layer < 0 || layer >= i.cfg.K() {
		return VerdictUnspecified, [32]byte{}, fmt.Errorf("twoab: %w: layer %d outside [0, %d)",
			ErrLayerOutOfRange, layer, i.cfg.K())
	}
	leaderID := i.cfg.Layers[layer].Leader
	retained := i.retainedBundles[layer][leaderID]

	if len(retained) >= 2 {
		// Equivocation observed — NR per spec §Phase 2a row 1.
		return VerdictNR, [32]byte{}, nil
	}
	if len(retained) == 0 {
		return VerdictNR, [32]byte{}, nil
	}
	// Exactly one retention. If it's auth-only, the operator can't issue
	// σV / NV on it (spec §Phase 1) — falls through to NR.
	single := retained[0]
	if single.AuthOnly {
		return VerdictNR, [32]byte{}, nil
	}
	// Regular retention — consult host verdict.
	valid, recorded := i.HostValidity(layer, single.Bundle.Value)
	if !recorded {
		return VerdictUnspecified, [32]byte{}, fmt.Errorf(
			"twoab: host validity not applied for retained V at layer %d "+
				"(call ApplyHostValidity before BuildVerdict)", layer)
	}
	root := ValueRoot(single.Bundle.Value)
	if valid {
		return VerdictSigmaV, root, nil
	}
	return VerdictNV, [32]byte{}, nil
}

// BuildVerdict constructs the local operator's Phase-2a Verdict for the
// given layer, based on the convergence-rule input state at the time of
// the call. Per spec §Phase 2a:
//
//   - Each operator emits exactly one Verdict per (slot, layer); a
//     second distinct Verdict would be Rule-6a self-equivocation.
//   - The Verdict envelope is op-identity-signed at the wire layer (not
//     in this method — the caller wraps the returned Verdict in a
//     SignedSSVMessage envelope at the SSV adapter layer).
//
// Idempotent: a second call with the same layer returns the cached
// verdict from the first call. The caller's runner is responsible for
// the broadcast schedule (T_verdict_max − ε_proc); calling BuildVerdict
// before host validity is applied for the retained V returns an error.
//
// The constructed Verdict is NOT self-observed. The caller broadcasts it
// to peers; the local op's own contribution to convergence comes from
// ownVerdict (cached here), not from peerVerdicts. Self-observation via
// ObserveVerdict is permitted but unnecessary — buildConvergencePools /
// l0SigmaEligibilityReached skip the local op's entry in peerVerdicts to
// avoid double-counting if a runner does choose to self-observe (the
// same defensive pattern as Phase 3's tryReconstructLayer). Mirrors
// Phase-E's BuildPhase1Bundle pattern.
func (i *Instance) BuildVerdict(layer int) (*Verdict, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if cached, ok := i.ownVerdict[layer]; ok {
		return cached, nil
	}
	kind, root, err := i.ComputeLocalVerdict(layer)
	if err != nil {
		return nil, err
	}
	v := &Verdict{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layer:      layer,
		Kind:       kind,
		ValueRoot:  root,
	}
	i.ownVerdict[layer] = v
	if layer == 0 {
		i.maybeSignalL0SigmaEligibility()
	}
	return v, nil
}

// ObserveVerdict records a peer's (or the local operator's own, after
// broadcast) Phase-2a Verdict. Per spec §Phase 2a / first-observed
// convergence:
//
//   - Bundles must pass structural validation (cluster id, slot, layer
//     range, sender in cluster, verdict-kind validity).
//
//   - First verdict observed from op at (slot, layer) → recorded in the
//     convergence pool (peerVerdicts[layer][op]).
//
//   - Subsequent verdicts from the same op at the same (slot, layer):
//
//   - Byte-identical re-broadcast (same content hash) → silent dedup.
//
//   - Distinct verdict → Rule 6a (verdict-vs-verdict equivocation)
//     evidence on the pair (first-observed, second-observed); op
//     is marked as verdictEquivocator at this layer (treat-as-null
//     state per spec §Phase 2a SHOULD rule; consumed by Phase G).
//
//   - Third+ distinct verdicts: dropped from convergence input, no
//     additional evidence (the (Rule 6a, op, layer) dedup ensures
//     one fire per equivocator per layer).
//
//   - The first-observed verdict is RETAINED in peerVerdicts even
//     after Rule 6a fires. Per spec: "Honest receivers count i's
//     first-observed verdict for convergence purposes; subsequent
//     verdicts are dropped from convergence input but recorded as
//     slashable." The treat-as-null state is consumed at convergence
//     time (Phase G) to override the first-observed contribution.
func (i *Instance) ObserveVerdict(v *Verdict) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidateVerdict(v, i.cfg); err != nil {
		return err
	}

	if i.peerVerdicts[v.Layer] == nil {
		i.peerVerdicts[v.Layer] = make(map[OperatorID]*Verdict)
	}
	existing, hasExisting := i.peerVerdicts[v.Layer][v.OperatorID]
	if !hasExisting {
		// First-observed verdict from this op at this layer.
		i.peerVerdicts[v.Layer][v.OperatorID] = deepCopyVerdict(v)
		if v.Layer == 0 {
			i.maybeSignalL0SigmaEligibility()
		}
		return nil
	}

	// Already have a verdict from this op. Check content equality.
	existingHash := verdictContentHash(existing)
	newHash := verdictContentHash(v)
	if existingHash == newHash {
		// Identical re-broadcast — silent dedup.
		return nil
	}

	// Distinct second verdict → Rule 6a (verdict equivocation).
	// Mark op as equivocator; convergence in Phase G treats their
	// contribution as null. The first-observed verdict stays in the
	// pool (per spec); the equivocator flag overrides at convergence
	// time.
	if i.verdictEquivocator[v.Layer] == nil {
		i.verdictEquivocator[v.Layer] = make(map[OperatorID]bool)
	}
	alreadyEquivocator := i.verdictEquivocator[v.Layer][v.OperatorID]
	i.verdictEquivocator[v.Layer][v.OperatorID] = true

	// Record evidence once per (op, layer) — the
	// (Rule, OperatorID, Layer) dedup in recordEvidence/EvidenceObserver
	// handles observer firing; we also gate the slice append here so the
	// Evidence() snapshot doesn't accumulate redundant entries from
	// third+ distinct verdicts.
	if !alreadyEquivocator {
		i.recordEvidence(Evidence{
			Rule:       EvidenceVerdictEquivocation,
			OperatorID: v.OperatorID,
			Layer:      v.Layer,
			VerdictEquivocation: &VerdictEquivocationEvidence{
				VerdictA: existing,
				VerdictB: deepCopyVerdict(v),
			},
		})
	}
	return nil
}

// IsEquivocator reports whether op has been observed broadcasting two or
// more distinct verdicts at the given layer. Phase G's convergence rule
// reads this to apply the treat-as-null SHOULD rule.
func (i *Instance) IsEquivocator(layer int, op OperatorID) bool {
	m := i.verdictEquivocator[layer]
	if m == nil {
		return false
	}
	return m[op]
}

// OwnVerdict returns the local operator's broadcast verdict at this
// layer, or (nil, false) if BuildVerdict hasn't been called yet.
func (i *Instance) OwnVerdict(layer int) (*Verdict, bool) {
	v, ok := i.ownVerdict[layer]
	return v, ok
}

// PeerVerdicts returns a snapshot of the first-observed verdicts per
// operator at the given layer. Callers MUST treat the returned map as
// read-only.
//
// The map is the underlying state at the call moment; the returned
// pointer-values are deep-copied so callers can't mutate Instance
// state through them. Subsequent Observe calls may add entries; this
// snapshot captures only what was observed by call time.
func (i *Instance) PeerVerdicts(layer int) map[OperatorID]*Verdict {
	src := i.peerVerdicts[layer]
	if src == nil {
		return nil
	}
	out := make(map[OperatorID]*Verdict, len(src))
	for op, v := range src {
		out[op] = deepCopyVerdict(v)
	}
	return out
}

// deepCopyVerdict returns an independent copy of v so retention/snapshot
// state isn't affected by caller-owned mutation. Verdict is all-value
// fields (ClusterID and ValueRoot are [32]byte arrays), so a struct
// value-copy suffices.
func deepCopyVerdict(v *Verdict) *Verdict {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
