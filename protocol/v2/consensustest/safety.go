package consensustest

import (
	"bytes"
	"fmt"
	"sort"
)

// SafetyReport captures the universal invariants the framework checks on
// every Outcome regardless of protocol or scenario. Any false field is a
// load-bearing safety violation; the framework panics on any of:
// SingleV, HonestAgreement, NoOfflineDoubleV, QuorumBackedDecision,
// NoEquivocationAccepted, OBFTCommitKindValid, OBFTHostValidityRespect,
// HonestCrossPhaseExclusive, HonestSingleSigmaV, HonestWalkConsistent.
//
// COVERAGE — what is actually instrumented today:
//
//   - SingleV, HonestAgreement, NoOfflineDoubleV are computed by the
//     framework from every Outcome's PerOp + OfflineAgg fields and
//     ALWAYS fire on a violation, regardless of adapter.
//   - HonestCrossPhaseExclusive and HonestSingleSigmaV are computed
//     from the OfflineAgg's by-emitter maps (SigmaByEmitter /
//     NRByEmitter — populated by OBFT-family adapters in
//     recordCommitToAggregator and friends), filtered to honest
//     operators via Outcome.Byz. Bucket-2 per-op invariants from
//     docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md. Empty by-emitter
//     maps (adapters that don't populate them, or synthetic-outcome
//     tests) iterate to zero violations — graceful default-true.
//   - HonestWalkConsistent is computed from each honest op's
//     OperatorOutcome.ResolveLayerAttempts trace (populated by
//     OBFT-family adapters from obft.Instance.LastResolveLayerAttempts),
//     filtered via Outcome.Byz. Bucket-3 D1 invariant. Empty trace
//     (non-OBFT-family adapter, or instrumented op where Resolve
//     never ran) iterates to zero violations — graceful default-true.
//   - NoEquivocationAccepted has EquivocationChecked=true in both
//     OBFT and 2abOBFT adapters, but EquivocationsAccepted is hard-
//     wired to 0 there because the adapters' internal Rule3 enforcement
//     excludes equivocating partials from σ/NR quorums by construction.
//     Any actually-accepted equivocation would manifest upstream as a
//     NoOfflineDoubleV / SingleV violation; this invariant is a
//     diagnostic on top of that catch.
//   - OBFTCommitKindValid has OBFTCommitKindChecked=true in both OBFT
//     and 2abOBFT adapters, with OBFTCommitKind set to "sigma" (decision
//     at L_0) or "nr" (decision at L_k>0) — always one of the two valid
//     values by construction, so the check is vacuously true. Useful as
//     a descriptive tag in panic diagnostics rather than a load-bearing
//     gate; a regression that ever sets an invalid kind would trip it.
//   - QuorumBackedDecision (Phase 2 C1) has QuorumChecked=true in both
//     OBFT and 2abOBFT adapters when at least one operator decided
//     locally (Round>=0) at the cluster-decided layer. QuorumSigners is
//     the SigmaPoolSize from that op's ResolveLayerAttempts at the
//     deciding layer — the protocol's own per-decision quorum count.
//     QuorumRequired = 2f+1. Defensive sentinel against a Resolve-gate
//     bypass: tryReconstructLayer only returns Output when poolSize >=
//     qV, so the check is vacuously satisfied under correct Resolve.
//     Stays uninstrumented (graceful) for cert-gossip-only clusters
//     where no op has a local-decider trace at the cluster's decided
//     layer.
//   - OBFTHostValidityRespect (Phase 2 C3) has OBFTHostValidityChecked=true
//     in both OBFT and 2abOBFT adapters whenever the cluster decided.
//     OBFTHostValidityRejecters counts honest, non-crashed σ-emitters
//     on the decided (layer, V) whose locked host verdict (from the
//     adapter's RecordingHostPattern wrapper, first-write-wins per
//     spec validate-once-and-lock) was invalid. A non-zero count is the
//     C3 violation: the protocol allowed σ-emission despite a
//     locked-invalid host verdict — bucket-2 HonestCrossPhaseExclusive
//     doesn't catch this because the regression produces no NR-collision.
//
// The defense-in-depth picture: a hypothetical adapter regression that
// accepted an equivocation or a sub-quorum certificate would surface as
// a SingleV / NoOfflineDoubleV violation upstream — those two are the
// load-bearing universal checks. HonestCrossPhaseExclusive +
// HonestSingleSigmaV catch the per-operator EKM regression class
// directly (rather than only transitively via NoOfflineDoubleV's
// "double-V reconstructable" end-state); HonestWalkConsistent catches
// Resolve-side regressions where an honest op's walk would have
// advanced past a σ-decidable layer (also rather than only transitively
// via NoOfflineDoubleV); QuorumBackedDecision is a defensive sentinel
// against a Resolve-gate-bypass that would return Output without genuine
// qV; OBFTHostValidityRespect catches the σ-emit-despite-invalid-host
// regression that HonestCrossPhaseExclusive doesn't fire on (no NR
// collision). NoEquivocationAccepted and OBFTCommitKindValid are
// layered diagnostics that distinguish "what went wrong" once the
// universal checks fire (or that future-proof the report against
// adapter changes).
type SafetyReport struct {
	// SingleV: at most one distinct Value is reconstructed cluster-wide
	// (Pigeonhole claim: "at most one full V signature per slot"). Round
	// metadata is allowed to differ across operators — see ComputeSafetyReport.
	SingleV bool

	// Terminated: every operator is either Decided or has a non-empty Err.
	// Protocols that leave ops waiting for events that will never fire fail
	// this check. Treated as a soft warning (not a safety violation) so the
	// matrix run continues.
	Terminated bool

	// HonestAgreement: all deciders agree on Value. Same check as SingleV
	// in this framework, kept as a separate field for diagnostic clarity.
	HonestAgreement bool

	// NoOfflineDoubleV: the offline aggregator (worst-case byzantine with
	// full message visibility) cannot reconstruct two distinct V signatures.
	// Strictly stronger than SingleV — catches cases where honest deciders
	// agree but a scheming aggregator could have built a second sig from
	// observed-but-not-locally-applied partials.
	NoOfflineDoubleV bool

	// QuorumBackedDecision: when the adapter instrumented its commit
	// certificate (Outcome.CommitAttestation.QuorumChecked), the decided
	// value is backed by ≥ QuorumRequired distinct valid signatures.
	// Default true (uninstrumented adapter ⇒ no violation reportable).
	QuorumBackedDecision bool

	// NoEquivocationAccepted: when the adapter instrumented equivocation
	// detection (Outcome.CommitAttestation.EquivocationChecked), no honest
	// validator committed based on an equivocating proposal in the same
	// (instance, round). Default true.
	NoEquivocationAccepted bool

	// OBFTCommitKindValid (OBFT-specific): when the adapter populated
	// Outcome.CommitAttestation.OBFTCommitKind, the value is either
	// "sigma" or "nr". Default true.
	OBFTCommitKindValid bool

	// OBFTHostValidityRespect (OBFT-specific): when the adapter
	// instrumented host-validity comparison, no honest validator's
	// predicate rejected the decided value. Default true.
	OBFTHostValidityRespect bool

	// HonestCrossPhaseExclusive: every honest operator emitted σ-XOR-NR
	// per (slot, layer) — no honest emitter appears in both
	// SigmaByEmitter and NRByEmitter at the same layer. Spec:
	// OBFT.md:411 (Pigeonhole 1, EKM-enforced cross-phase exclusivity).
	// Subsumes B3 (leader-σ locks σ-side, layer's leader cannot emit
	// NR/NV for own layer) since the leader's σ_V observation is
	// recorded by-emitter alongside the NR partial that would be the
	// violation. Default true. Computed from OfflineAgg by-emitter
	// maps; honest filter via Outcome.Byz.
	HonestCrossPhaseExclusive bool

	// HonestSingleSigmaV: every honest operator emitted at most one
	// σ-on-V per (slot, layer) — no honest emitter appears in
	// SigmaByEmitter at the same layer with two distinct value_hashes.
	// Spec: OBFT.md:411 (single-σ-V exclusivity, EKM-enforced).
	// Default true. Computed from OfflineAgg by-emitter maps; honest
	// filter via Outcome.Byz.
	HonestSingleSigmaV bool

	// HonestWalkConsistent: every honest decider's Phase-3 walk visited
	// layers consistently with the per-op decision outcome. Two
	// violation cases (D1 from docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md):
	//   (a) Decided=true + ResolveLayerAttempts shows no σ-reached layer
	//       AND cluster-wide aggregator never reconstructed this V — i.e.,
	//       decision without any plausible σ-quorum source. Legitimate
	//       cert-gossip-decide is the legal case here (cluster
	//       reconstructed σ-quorum at some layer/V, this op caught up
	//       via Certificate without local Resolve); the
	//       clusterReachedSigmaQuorumAt lookup distinguishes the two.
	//   (b) Decided=true + decided at a layer DEEPER than the shallowest
	//       σ-reached layer in the trace — i.e., walk advanced past a
	//       σ-decidable layer. Resolve-side regression. Skipped when
	//       oo.Round == -1 (cert-gossip-decide: the op's local trace may
	//       show a σ-reached layer where Resolve internally decided, but
	//       the adapter stamps Round=-1 for cert-gossip; this is not a
	//       regression).
	// Default true. Skipped per op when ResolveLayerAttempts is empty
	// (graceful degradation for adapters / protocol families without a
	// layered walk — e.g., QBFT / PSigs) or when ClipLateDecision turned
	// off oo.Decided post-Resolve (the op's internal Resolve still
	// succeeded; ClipLate is a deadline check, not a regression).
	HonestWalkConsistent bool

	// DistinctOutputs records (Round, Value) pairs for diagnostic dumps;
	// length > 1 means SingleV was violated.
	DistinctOutputs []OutputTuple

	// CrossPhaseEvidence enumerates honest (op, layer) pairs that
	// violated cross-phase exclusivity. Non-empty iff
	// HonestCrossPhaseExclusive=false.
	CrossPhaseEvidence []CrossPhaseViolation

	// SingleSigmaVEvidence enumerates honest (op, layer, two value_hashes)
	// triples that violated single-σ-V. Non-empty iff
	// HonestSingleSigmaV=false.
	SingleSigmaVEvidence []SingleSigmaVViolation

	// WalkConsistencyEvidence enumerates the per-op walk-consistency
	// violations. Non-empty iff HonestWalkConsistent=false.
	WalkConsistencyEvidence []WalkConsistencyViolation
}

// CrossPhaseViolation identifies one honest operator that emitted both
// σ-side and NR-side at the same layer.
type CrossPhaseViolation struct {
	Operator OperatorID
	Layer    int
}

// SingleSigmaVViolation identifies one honest operator that emitted σ
// on two distinct V's at the same layer. ValueHashA and ValueHashB are
// the lex-smallest pair of distinct value hashes found for this
// (operator, layer) — ComputeSafetyReport sorts the gathered hashes
// before picking the first two, so the recorded pair is deterministic
// across runs even though Go map iteration is randomized. An operator
// with > 2 V's would only surface the lex-smallest pair; the violation
// is unambiguous regardless of which pair is reported.
type SingleSigmaVViolation struct {
	Operator   OperatorID
	Layer      int
	ValueHashA [32]byte
	ValueHashB [32]byte
}

// WalkConsistencyViolation describes one honest operator's walk-state
// inconsistency. Reason names the violation case (a, b, or c — see
// HonestWalkConsistent's docstring). DecidedLayer is -1 when the op
// did not decide. SigmaReachedLayers is the sorted-ascending list of
// layers where the op's local σ-pool view reached qV during Resolve.
type WalkConsistencyViolation struct {
	Operator           OperatorID
	Reason             WalkInconsistencyReason
	DecidedLayer       int
	SigmaReachedLayers []int
}

// WalkInconsistencyReason names the two D1 violation cases.
type WalkInconsistencyReason int

const (
	// WalkDecidedNoSigmaSource — op decided but ResolveLayerAttempts
	// shows no σ-reached layer AND the cluster-wide aggregator never
	// reconstructed this V. Decision without any plausible σ-quorum
	// source — neither local Resolve nor cluster-wide reconstruction.
	WalkDecidedNoSigmaSource WalkInconsistencyReason = iota + 1
	// WalkAdvancedPastSigma — op decided at a layer DEEPER than the
	// shallowest σ-reached layer in the trace. Walk advanced past a
	// σ-decidable layer.
	WalkAdvancedPastSigma
)

// String returns the spec-aligned label for the violation case.
func (r WalkInconsistencyReason) String() string {
	switch r {
	case WalkDecidedNoSigmaSource:
		return "decided-no-sigma-source"
	case WalkAdvancedPastSigma:
		return "advanced-past-sigma"
	default:
		return "unknown"
	}
}

// IsViolation reports whether any load-bearing safety property is false.
// Terminated is excluded (soft warning, see SafetyReport.Terminated).
//
// Today's effective coverage (per the SafetyReport doc): SingleV,
// HonestAgreement, NoOfflineDoubleV, HonestCrossPhaseExclusive, and
// HonestSingleSigmaV are the ones that can actually fire across the
// adapter set. The remaining four are kept here so a future adapter
// that opts into the corresponding *Checked gate participates in the
// panic path without a framework-side change; until then they default
// to true and don't contribute violations.
func (r SafetyReport) IsViolation() bool {
	return !r.SingleV ||
		!r.HonestAgreement ||
		!r.NoOfflineDoubleV ||
		!r.QuorumBackedDecision ||
		!r.NoEquivocationAccepted ||
		!r.OBFTCommitKindValid ||
		!r.OBFTHostValidityRespect ||
		!r.HonestCrossPhaseExclusive ||
		!r.HonestSingleSigmaV ||
		!r.HonestWalkConsistent
}

type OutputTuple struct {
	Round int
	Value []byte
}

// String renders a one-line summary; non-OK fields go first.
func (r SafetyReport) String() string {
	if r.SingleV && r.Terminated && r.HonestAgreement && r.NoOfflineDoubleV &&
		r.QuorumBackedDecision && r.NoEquivocationAccepted &&
		r.OBFTCommitKindValid && r.OBFTHostValidityRespect &&
		r.HonestCrossPhaseExclusive && r.HonestSingleSigmaV &&
		r.HonestWalkConsistent {
		return "SAFETY OK"
	}
	parts := []string{}
	if !r.SingleV {
		parts = append(parts, fmt.Sprintf("SingleV=FAIL (%d distinct outputs)", len(r.DistinctOutputs)))
	}
	if !r.NoOfflineDoubleV {
		parts = append(parts, "NoOfflineDoubleV=FAIL (offline aggregator could rebuild ≥ 2 V sigs)")
	}
	if !r.HonestCrossPhaseExclusive {
		parts = append(parts, fmt.Sprintf("HonestCrossPhaseExclusive=FAIL (%d honest op(s) emitted σ AND NR at same layer)", len(r.CrossPhaseEvidence)))
	}
	if !r.HonestSingleSigmaV {
		parts = append(parts, fmt.Sprintf("HonestSingleSigmaV=FAIL (%d honest op(s) emitted σ on two distinct V's at same layer)", len(r.SingleSigmaVEvidence)))
	}
	if !r.HonestWalkConsistent {
		parts = append(parts, fmt.Sprintf("HonestWalkConsistent=FAIL (%d honest op(s) walked inconsistently with decision)", len(r.WalkConsistencyEvidence)))
	}
	if !r.QuorumBackedDecision {
		parts = append(parts, "QuorumBackedDecision=FAIL (decision lacks quorum-sized signature set)")
	}
	if !r.NoEquivocationAccepted {
		parts = append(parts, "NoEquivocationAccepted=FAIL (honest validator committed on equivocating proposal)")
	}
	if !r.OBFTCommitKindValid {
		parts = append(parts, "OBFTCommitKindValid=FAIL (commit not justified by σ-quorum or NR-quorum)")
	}
	if !r.OBFTHostValidityRespect {
		parts = append(parts, "OBFTHostValidityRespect=FAIL (decided value rejected by some honest validator's host-validity predicate)")
	}
	if !r.Terminated {
		parts = append(parts, "Terminated=FAIL (some operator still in-flight)")
	}
	if !r.HonestAgreement {
		parts = append(parts, "HonestAgreement=FAIL (deciders disagreed on output)")
	}
	out := "SAFETY:"
	for _, p := range parts {
		out += " " + p
	}
	return out
}

// ComputeSafetyReport runs the universal invariants over an Outcome.
// SingleV checks Value only (not (Round, Value)) because some adapters set
// Round=-1 for operators that decided via certificate-gossip fallback (the
// cert carries V+sig but not the originating round). DistinctOutputs still
// records (Round, Value) so real safety violations print both rounds.
//
// NoOfflineDoubleV is read from o.OfflineAgg (set by adapters that
// instrument the aggregator). When unset (zero value), defaults to true so
// adapters not yet instrumenting the aggregator don't trigger spurious
// safety panics. The same graceful-degradation pattern applies to the
// CommitAttestation-driven invariants (QuorumBackedDecision,
// NoEquivocationAccepted, OBFTCommitKindValid, OBFTHostValidityRespect):
// adapters set the corresponding *Checked field to opt in, and only then
// can a violation be reported.
func ComputeSafetyReport(o Outcome) SafetyReport {
	r := SafetyReport{
		SingleV:                   true,
		Terminated:                true,
		HonestAgreement:           true,
		NoOfflineDoubleV:          true,
		QuorumBackedDecision:      true,
		NoEquivocationAccepted:    true,
		OBFTCommitKindValid:       true,
		OBFTHostValidityRespect:   true,
		HonestCrossPhaseExclusive: true,
		HonestSingleSigmaV:        true,
		HonestWalkConsistent:      true,
	}

	distinctValues := [][]byte{}
	for _, oo := range o.PerOp {
		if !oo.Decided {
			continue
		}
		seenV := false
		for _, v := range distinctValues {
			if bytes.Equal(v, oo.Value) {
				seenV = true
				break
			}
		}
		if !seenV {
			distinctValues = append(distinctValues, append([]byte(nil), oo.Value...))
		}
		seenTuple := false
		for _, t := range r.DistinctOutputs {
			if t.Round == oo.Round && bytes.Equal(t.Value, oo.Value) {
				seenTuple = true
				break
			}
		}
		if !seenTuple {
			r.DistinctOutputs = append(r.DistinctOutputs, OutputTuple{
				Round: oo.Round,
				Value: append([]byte(nil), oo.Value...),
			})
		}
	}

	if len(distinctValues) > 1 {
		r.SingleV = false
		r.HonestAgreement = false
	}

	for _, oo := range o.PerOp {
		if !oo.Decided && oo.Err == "" {
			r.Terminated = false
			break
		}
	}

	// Adapters that populate OfflineAgg report their verdict via
	// OfflineAgg.NoOfflineDoubleV. The OfflineAggReport zero value has
	// NoOfflineDoubleV=false; we treat zero-value (no Reconstructions
	// recorded AND zero NoOfflineDoubleV) as "adapter didn't instrument" =
	// no violation reportable.
	if o.OfflineAgg.NoOfflineDoubleV {
		// adapter ran the aggregator and confirmed safety
		r.NoOfflineDoubleV = true
	} else if len(o.OfflineAgg.Reconstructions) > 0 {
		// adapter ran the aggregator and found a violation
		r.NoOfflineDoubleV = false
	}
	// else: zero value, adapter didn't instrument; leave true.

	// Per-decision invariants read CommitAttestation. Each *Checked bool
	// gates the corresponding check; uninstrumented invariants stay at
	// default true. Adapter migration plan: see protocol.go docstring.
	att := o.CommitAttestation
	if att.QuorumChecked && o.Decided {
		if att.QuorumRequired > 0 && att.QuorumSigners < att.QuorumRequired {
			r.QuorumBackedDecision = false
		}
	}
	if att.EquivocationChecked && att.EquivocationsAccepted > 0 {
		r.NoEquivocationAccepted = false
	}
	if att.OBFTCommitKindChecked && o.Decided {
		if att.OBFTCommitKind != "sigma" && att.OBFTCommitKind != "nr" {
			r.OBFTCommitKindValid = false
		}
	}
	if att.OBFTHostValidityChecked && o.Decided {
		if att.OBFTHostValidityRejecters > 0 {
			r.OBFTHostValidityRespect = false
		}
	}

	// B1 — Cross-phase exclusivity per honest emitter: no honest op
	// appears in both SigmaByEmitter and NRByEmitter at the same layer.
	// Spec: OBFT.md:411 Pigeonhole 1. Subsumes B3 (layer leader's
	// Phase-1 σ_V counts toward σ-side, so a leader who NR/NV's their
	// own layer triggers the same collision).
	//
	// Iteration ordering is non-deterministic across Go's map; we sort
	// the resulting evidence slice for deterministic panic messages
	// (the existing pattern in SafetyPanic).
	for sigKey := range o.OfflineAgg.SigmaByEmitter {
		op := sigKey.Emitter
		if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
			continue
		}
		nrKey := ByEmitterNRKey{Emitter: op, Layer: sigKey.Layer}
		if _, hasNR := o.OfflineAgg.NRByEmitter[nrKey]; hasNR {
			r.HonestCrossPhaseExclusive = false
			r.CrossPhaseEvidence = append(r.CrossPhaseEvidence,
				CrossPhaseViolation{Operator: op, Layer: sigKey.Layer})
		}
	}
	// Dedup CrossPhaseEvidence (multiple σ entries for the same (op,
	// layer) on different V's would otherwise produce duplicate
	// records). The B1 violation is "this op did the σ+NR collision",
	// not "this op did it N times".
	r.CrossPhaseEvidence = dedupCrossPhaseEvidence(r.CrossPhaseEvidence)
	sort.Slice(r.CrossPhaseEvidence, func(i, j int) bool {
		if r.CrossPhaseEvidence[i].Operator != r.CrossPhaseEvidence[j].Operator {
			return r.CrossPhaseEvidence[i].Operator < r.CrossPhaseEvidence[j].Operator
		}
		return r.CrossPhaseEvidence[i].Layer < r.CrossPhaseEvidence[j].Layer
	})

	// B2 — Single-σ-V per honest emitter per layer: no honest op
	// appears in SigmaByEmitter at the same layer with two distinct
	// value_hashes. Spec: OBFT.md:411 single-σ-V exclusivity.
	type opLayerKey struct {
		Op    OperatorID
		Layer int
	}
	sigmaByOpLayer := map[opLayerKey][][32]byte{}
	for sigKey := range o.OfflineAgg.SigmaByEmitter {
		op := sigKey.Emitter
		if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
			continue
		}
		k := opLayerKey{Op: op, Layer: sigKey.Layer}
		sigmaByOpLayer[k] = append(sigmaByOpLayer[k], sigKey.ValueHash)
	}
	for k, hashes := range sigmaByOpLayer {
		if len(hashes) > 1 {
			r.HonestSingleSigmaV = false
			// First two distinct hashes (lex-sorted for determinism).
			sort.Slice(hashes, func(i, j int) bool {
				return bytes.Compare(hashes[i][:], hashes[j][:]) < 0
			})
			r.SingleSigmaVEvidence = append(r.SingleSigmaVEvidence,
				SingleSigmaVViolation{
					Operator:   k.Op,
					Layer:      k.Layer,
					ValueHashA: hashes[0],
					ValueHashB: hashes[1],
				})
		}
	}
	sort.Slice(r.SingleSigmaVEvidence, func(i, j int) bool {
		if r.SingleSigmaVEvidence[i].Operator != r.SingleSigmaVEvidence[j].Operator {
			return r.SingleSigmaVEvidence[i].Operator < r.SingleSigmaVEvidence[j].Operator
		}
		return r.SingleSigmaVEvidence[i].Layer < r.SingleSigmaVEvidence[j].Layer
	})

	// D1 — Per-op walk-state consistency: every honest decider's
	// ResolveLayerAttempts must be consistent with their decision. See
	// HonestWalkConsistent docstring for the three violation cases.
	// Skipped per op when ResolveLayerAttempts is empty (adapter / proto
	// family doesn't instrument — graceful default).
	//
	// Iterate o.PerOp deterministically by sorted op ID so the panic
	// message ordering is stable across runs.
	opIDs := make([]OperatorID, 0, len(o.PerOp))
	for op := range o.PerOp {
		opIDs = append(opIDs, op)
	}
	sort.Slice(opIDs, func(i, j int) bool { return opIDs[i] < opIDs[j] })
	for _, op := range opIDs {
		oo := o.PerOp[op]
		if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
			continue
		}
		if len(oo.ResolveLayerAttempts) == 0 {
			continue // graceful default — adapter didn't instrument
		}
		sigmaReachedAt := []int{}
		for _, la := range oo.ResolveLayerAttempts {
			if la.SigmaReached {
				sigmaReachedAt = append(sigmaReachedAt, la.Layer)
			}
		}
		// Skip when ClipLateDecision turned off oo.Decided post-Resolve.
		// Strictly redundant under the current check shape — both case
		// (a) and case (b) below gate on oo.Decided, so a non-decided
		// op already short-circuits via either check. Kept defensively:
		// any future walk-consistency case that DOESN'T gate on
		// oo.Decided (e.g., "trace shows σ-reached layer K, claims to
		// have decided at L, but PerOp.Decided is false") would
		// otherwise false-flag clip-late-decided ops where the op's
		// internal Resolve genuinely succeeded.
		if !oo.Decided && oo.Err == ErrMissedRelayDeadline {
			continue
		}
		// Case (a): Decided=true + no σ-reached layer locally. Split
		// by Round semantics:
		//   - oo.Round >= 0 (local-decide claim): the op asserts they
		//     reconstructed locally at this layer. The trace MUST
		//     confirm via a σ-reached entry. Empty trace = real
		//     regression (Resolve or adapter mis-recorded the decision).
		//   - oo.Round == -1 (cert-gossip-decide): the op caught up via
		//     a Certificate from another op's local-decide. Legitimate
		//     iff the cluster has some OTHER op marked as local-decider
		//     on this V; if no operator anywhere reached σ-quorum
		//     locally, the cert is bogus.
		if oo.Decided && len(sigmaReachedAt) == 0 {
			legitimate := false
			if oo.Round == -1 {
				// Cert-gossip-decide path — search for a cluster local
				// decider on V (excluding the op under question, since
				// it claims cert-decide and isn't a local-decider).
				legitimate = clusterLocalDecidedOn(o, op, oo.Value)
			}
			if !legitimate {
				r.HonestWalkConsistent = false
				r.WalkConsistencyEvidence = append(r.WalkConsistencyEvidence,
					WalkConsistencyViolation{
						Operator:           op,
						Reason:             WalkDecidedNoSigmaSource,
						DecidedLayer:       oo.Round,
						SigmaReachedLayers: sigmaReachedAt,
					})
			}
			continue
		}
		// Case (b): Decided=true at layer K > min(sigmaReachedAt). Walk
		// advanced past a σ-reachable layer. ResolveLayerAttempts is
		// appended in walk order so sigmaReachedAt is ascending; the
		// shallowest σ-reached layer is sigmaReachedAt[0].
		//
		// Uses `!=` rather than `>` as belt-and-suspenders. The `<`
		// case (PerOp.Round shallower than min sigmaReachedAt) is
		// impossible by construction under correct Resolve: Resolve
		// returns Output at the first σ-reached layer, so sigmaReachedAt
		// always contains Round under correct walks. The `!=` catches
		// both the spec-aligned "advanced past" regression AND a
		// hypothetical adapter inconsistency where PerOp.Round
		// disagrees with the trace.
		//
		// Skipped when oo.Round == -1 (cert-gossip-decide stamps -1 as
		// "layer unknown to this op since they didn't reconstruct
		// locally" — the op may have a σ-reached trace from a parallel
		// local Resolve that errored out before completing; the cluster
		// decided via different ops' σ-quorum and gossiped a cert; not
		// a regression).
		if oo.Decided && oo.Round >= 0 && len(sigmaReachedAt) > 0 && oo.Round != sigmaReachedAt[0] {
			r.HonestWalkConsistent = false
			r.WalkConsistencyEvidence = append(r.WalkConsistencyEvidence,
				WalkConsistencyViolation{
					Operator:           op,
					Reason:             WalkAdvancedPastSigma,
					DecidedLayer:       oo.Round,
					SigmaReachedLayers: sigmaReachedAt,
				})
		}
	}

	return r
}

// clusterLocalDecidedOn reports whether any operator OTHER than
// `exclude` is marked as locally-decided (PerOp.Decided=true with
// PerOp.Round>=0) on V. Used by D1's case-(a) cert-gossip branch to
// distinguish legitimate cluster catch-up (some other op
// reconstructed σ-quorum locally → gossipped a cert → `exclude` op
// applied the cert) from a bogus-cert regression (no operator
// anywhere reached σ-quorum locally but `exclude` op claims a
// cert-decide).
//
// Resolve only returns Output on real σ-quorum (the protocol's
// load-bearing invariant), so a local-decide (Round>=0) implies
// σ-quorum was reached at that operator. The cluster's own verdict is
// the authoritative signal — the offline aggregator's
// Reconstructions / SigmaCardinality are conservative
// under-approximations of protocol-side σ-pool reconstruction (they
// check each source independently; the protocol combines).
//
// `exclude` is the op under question — we don't want to use it as its
// own evidence of legitimacy (case-(a) is specifically about op
// claiming a cert-decide with no local source).
//
// Graceful default: returns true when PerOp is empty (no operator data
// available — can't disambiguate; assume legitimate to avoid false
// positives).
func clusterLocalDecidedOn(o Outcome, exclude OperatorID, v []byte) bool {
	// Unreachable from D1's caller (it iterates PerOp; an empty map
	// never enters the loop body). Defensive for direct callers / future
	// reuse — empty PerOp means "no data to disambiguate", so return
	// true (legitimate) to avoid false-flagging.
	if len(o.PerOp) == 0 {
		return true
	}
	for op, oo := range o.PerOp {
		if op == exclude {
			continue
		}
		if oo.Decided && oo.Round >= 0 && bytes.Equal(oo.Value, v) {
			return true
		}
	}
	return false
}

// dedupCrossPhaseEvidence collapses duplicate (op, layer) records that
// arise when an op has multiple σ entries (different V's) at the same
// layer that also collide with an NR entry. The B1 violation is the
// collision itself; multiplicity isn't informative.
func dedupCrossPhaseEvidence(evs []CrossPhaseViolation) []CrossPhaseViolation {
	if len(evs) <= 1 {
		return evs
	}
	seen := make(map[CrossPhaseViolation]struct{}, len(evs))
	out := evs[:0]
	for _, e := range evs {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// SafetyPanic panics with a structured diagnostic. Should never fire on a
// correct protocol implementation. expected is the scenario's declared
// per-protocol expectation (e.g. ExpectSuccessFastest); a per-op evidence
// summary, the CommitAttestation diagnostic fields, and the trace (when
// enabled) are appended so a violating run is self-diagnosing.
//
// seed is the SimConfig.Seed that produced this Outcome — included in the
// panic message so the failing sim is reproducible by re-running with
// (scenario, protocol, seed) without re-iterating the batch.
func SafetyPanic(report SafetyReport, scenarioName, protocolName string, expected ExpectClass, seed int64, o Outcome) {
	msg := fmt.Sprintf(
		"CONSENSUSTEST SAFETY VIOLATION\nscenario=%s protocol=%s seed=%d expected=%s\n  %s\n  outcome: decided=%v round=%d value=%x\n  distinct outputs: %v\n  %s",
		scenarioName, protocolName, seed, expected,
		report,
		o.Decided, o.DecidedRound, o.DecidedValue,
		report.DistinctOutputs,
		o.OfflineAgg,
	)
	// Surface the CommitAttestation diagnostic fields whenever any of the
	// attestation-driven invariants OR the per-op honest invariants
	// (B1/B2/D1) fired. Equivocation counts + quorum sizes are useful
	// for disambiguating B1/B2 panics ("honest cross-signed vs. byz
	// equivocated and Rule 3 caught some") and for D1 panics
	// ("decided without σ-source vs. quorum-short reconstruction"),
	// not just for the C-invariant failures. Without these, panic
	// messages would say "FAIL" without showing the offending
	// counts/kind.
	att := o.CommitAttestation
	if !report.QuorumBackedDecision || !report.NoEquivocationAccepted ||
		!report.OBFTCommitKindValid || !report.OBFTHostValidityRespect ||
		!report.HonestCrossPhaseExclusive || !report.HonestSingleSigmaV ||
		!report.HonestWalkConsistent {
		msg += fmt.Sprintf(
			"\n  attestation: quorumSigners=%d quorumRequired=%d equivObserved=%d equivAccepted=%d obftCommitKind=%q obftHostValidityRejecters=%d",
			att.QuorumSigners, att.QuorumRequired,
			att.EquivocationsObserved, att.EquivocationsAccepted,
			att.OBFTCommitKind, att.OBFTHostValidityRejecters,
		)
	}
	// Surface per-op evidence for the by-emitter invariants. Without
	// these, B1/B2 panics would say "FAIL (N op(s))" without naming
	// which ops at which layers.
	if !report.HonestCrossPhaseExclusive {
		msg += "\n  cross-phase evidence:"
		for _, e := range report.CrossPhaseEvidence {
			msg += fmt.Sprintf(" op=%d layer=%d;", e.Operator, e.Layer)
		}
	}
	if !report.HonestSingleSigmaV {
		msg += "\n  single-σ-V evidence:"
		for _, e := range report.SingleSigmaVEvidence {
			msg += fmt.Sprintf(" op=%d layer=%d V_a=%x V_b=%x;",
				e.Operator, e.Layer, e.ValueHashA[:6], e.ValueHashB[:6])
		}
	}
	if !report.HonestWalkConsistent {
		msg += "\n  walk-consistency evidence:"
		for _, e := range report.WalkConsistencyEvidence {
			msg += fmt.Sprintf(" op=%d reason=%s decided-layer=%d σ-reached=%v;",
				e.Operator, e.Reason, e.DecidedLayer, e.SigmaReachedLayers)
		}
	}
	// Iterate ops in sorted order so the panic message is deterministic
	// across runs (Go map iteration is randomized). EvidenceByRule's %v
	// formatting is already sorted by Go's printer for map types.
	opIDs := make([]OperatorID, 0, len(o.PerOp))
	for op := range o.PerOp {
		opIDs = append(opIDs, op)
	}
	sort.Slice(opIDs, func(i, j int) bool { return opIDs[i] < opIDs[j] })
	for _, op := range opIDs {
		oo := o.PerOp[op]
		if len(oo.EvidenceByRule) > 0 {
			msg += fmt.Sprintf("\n  op=%d evidence: %v", op, oo.EvidenceByRule)
		}
	}
	if len(o.Trace) > 0 {
		msg += "\n  trace:"
		for _, e := range o.Trace {
			msg += fmt.Sprintf("\n    %v %s", e.When, e.Event)
		}
	}
	panic(msg)
}
