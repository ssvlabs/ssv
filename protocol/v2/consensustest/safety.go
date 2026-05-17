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
// NoEquivocationAccepted, OBFTCommitKindValid, OBFTHostValidityRespect.
//
// COVERAGE — what is actually instrumented today:
//
//   - SingleV, HonestAgreement, NoOfflineDoubleV are computed by the
//     framework from every Outcome's PerOp + OfflineAgg fields and
//     ALWAYS fire on a violation, regardless of adapter.
//   - NoEquivocationAccepted has EquivocationChecked=true in both
//     OBFT and 2abOBFT adapters, but EquivocationsAccepted is hard-
//     wired to 0 there because the adapters' internal Rule3 enforcement
//     excludes equivocating partials from σ/NR quorums by construction.
//     Any actually-accepted equivocation would manifest upstream as a
//     NoOfflineDoubleV / SingleV violation; this invariant is a
//     diagnostic on top of that catch.
//   - QuorumBackedDecision, OBFTCommitKindValid, OBFTHostValidityRespect
//     are advertised invariants whose *Checked gates are NOT set by any
//     adapter today. They default to true and CANNOT fire on a
//     violation under the current adapters. They are kept in the struct
//     so a future adapter can opt in without a framework-side change,
//     and so a regression that ever toggles a *Checked flag without
//     also routing the predicate through still trips IsViolation. See
//     each adapter's computeAttestation docstring for the "left
//     uninstrumented" rationale and deferral notes.
//
// The defense-in-depth picture: a hypothetical adapter regression that
// accepted an equivocation or a sub-quorum certificate would surface as
// a SingleV / NoOfflineDoubleV violation upstream — those two are the
// load-bearing universal checks. The five protocol-specific invariants
// are layered diagnostics that distinguish "what went wrong" once the
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

	// DistinctOutputs records (Round, Value) pairs for diagnostic dumps;
	// length > 1 means SingleV was violated.
	DistinctOutputs []OutputTuple
}

// IsViolation reports whether any load-bearing safety property is false.
// Terminated is excluded (soft warning, see SafetyReport.Terminated).
//
// Today's effective coverage (per the SafetyReport doc): SingleV +
// HonestAgreement + NoOfflineDoubleV are the ones that can actually
// fire across the adapter set. The remaining four are kept here so a
// future adapter that opts into the corresponding *Checked gate
// participates in the panic path without a framework-side change; until
// then they default to true and don't contribute violations.
func (r SafetyReport) IsViolation() bool {
	return !r.SingleV ||
		!r.HonestAgreement ||
		!r.NoOfflineDoubleV ||
		!r.QuorumBackedDecision ||
		!r.NoEquivocationAccepted ||
		!r.OBFTCommitKindValid ||
		!r.OBFTHostValidityRespect
}

type OutputTuple struct {
	Round int
	Value []byte
}

// String renders a one-line summary; non-OK fields go first.
func (r SafetyReport) String() string {
	if r.SingleV && r.Terminated && r.HonestAgreement && r.NoOfflineDoubleV &&
		r.QuorumBackedDecision && r.NoEquivocationAccepted &&
		r.OBFTCommitKindValid && r.OBFTHostValidityRespect {
		return "SAFETY OK"
	}
	parts := []string{}
	if !r.SingleV {
		parts = append(parts, fmt.Sprintf("SingleV=FAIL (%d distinct outputs)", len(r.DistinctOutputs)))
	}
	if !r.NoOfflineDoubleV {
		parts = append(parts, "NoOfflineDoubleV=FAIL (offline aggregator could rebuild ≥ 2 V sigs)")
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
		SingleV:                 true,
		Terminated:              true,
		HonestAgreement:         true,
		NoOfflineDoubleV:        true,
		QuorumBackedDecision:    true,
		NoEquivocationAccepted:  true,
		OBFTCommitKindValid:     true,
		OBFTHostValidityRespect: true,
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

	return r
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
	// attestation-driven invariants fired. Without these, panic messages
	// for quorum / equivocation / σ-or-NR / host-validity failures would
	// say "FAIL" without showing the offending counts/kind.
	att := o.CommitAttestation
	if !report.QuorumBackedDecision || !report.NoEquivocationAccepted ||
		!report.OBFTCommitKindValid || !report.OBFTHostValidityRespect {
		msg += fmt.Sprintf(
			"\n  attestation: quorumSigners=%d quorumRequired=%d equivObserved=%d equivAccepted=%d obftCommitKind=%q obftHostValidityRejecters=%d",
			att.QuorumSigners, att.QuorumRequired,
			att.EquivocationsObserved, att.EquivocationsAccepted,
			att.OBFTCommitKind, att.OBFTHostValidityRejecters,
		)
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
