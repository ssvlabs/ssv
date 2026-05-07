package consensustest

import (
	"bytes"
	"fmt"
	"sort"
)

// SafetyReport captures the universal invariants the framework checks on
// every Outcome regardless of protocol or scenario. Any false field is a
// load-bearing safety violation; the framework panics on SingleV /
// HonestAgreement / NoOfflineDoubleV breaches.
type SafetyReport struct {
	// SingleV: at most one distinct Value is reconstructed cluster-wide
	// (Pigeonhole claim: "at most one full V signature per slot"). Round
	// metadata is allowed to differ across operators — see ComputeSafetyReport.
	SingleV bool

	// Terminated: every operator is either Decided or has a non-empty Err.
	// Protocols that leave ops waiting for events that will never fire fail
	// this check.
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

	// DistinctOutputs records (Round, Value) pairs for diagnostic dumps;
	// length > 1 means SingleV was violated.
	DistinctOutputs []OutputTuple
}

type OutputTuple struct {
	Round int
	Value []byte
}

// String renders a one-line summary; non-OK fields go first.
func (r SafetyReport) String() string {
	if r.SingleV && r.Terminated && r.HonestAgreement && r.NoOfflineDoubleV {
		return "SAFETY OK"
	}
	parts := []string{}
	if !r.SingleV {
		parts = append(parts, fmt.Sprintf("SingleV=FAIL (%d distinct outputs)", len(r.DistinctOutputs)))
	}
	if !r.NoOfflineDoubleV {
		parts = append(parts, "NoOfflineDoubleV=FAIL (offline aggregator could rebuild ≥ 2 V sigs)")
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
// safety panics — Phase 1 + Phase 2 wire this up.
func ComputeSafetyReport(o Outcome) SafetyReport {
	r := SafetyReport{
		SingleV:          true,
		Terminated:       true,
		HonestAgreement:  true,
		NoOfflineDoubleV: true,
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

	return r
}

// SafetyPanic panics with a structured diagnostic. Should never fire on a
// correct protocol implementation. expected is the scenario's declared
// per-protocol expectation (e.g. ExpectSuccessFastest); a per-op evidence
// summary and the trace (when enabled) are appended so a violating run is
// self-diagnosing.
func SafetyPanic(report SafetyReport, scenarioName, protocolName string, expected ExpectClass, o Outcome) {
	msg := fmt.Sprintf(
		"CONSENSUSTEST SAFETY VIOLATION\nscenario=%s protocol=%s expected=%s\n  %s\n  outcome: decided=%v round=%d value=%x\n  distinct outputs: %v\n  %s",
		scenarioName, protocolName, expected,
		report,
		o.Decided, o.DecidedRound, o.DecidedValue,
		report.DistinctOutputs,
		o.OfflineAgg,
	)
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
