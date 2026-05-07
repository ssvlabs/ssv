package consensustest

import (
	"bytes"
	"fmt"
)

// SafetyReport captures the universal invariants the framework checks on
// every Outcome regardless of protocol or scenario. Any false field is a
// load-bearing safety violation; the framework panics on SingleV /
// HonestAgreement breaches.
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
	if r.SingleV && r.Terminated && r.HonestAgreement {
		return "SAFETY OK"
	}
	parts := []string{}
	if !r.SingleV {
		parts = append(parts, fmt.Sprintf("SingleV=FAIL (%d distinct outputs)", len(r.DistinctOutputs)))
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
func ComputeSafetyReport(o Outcome) SafetyReport {
	r := SafetyReport{
		SingleV:         true,
		Terminated:      true,
		HonestAgreement: true,
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
	return r
}

// SafetyPanic panics with a structured diagnostic. Should never fire on a
// correct protocol implementation.
func SafetyPanic(report SafetyReport, scenarioName, protocolName string, o Outcome) {
	panic(fmt.Sprintf(
		"CONSENSUSTEST SAFETY VIOLATION\nscenario=%s protocol=%s\n  %s\n  outcome: decided=%v round=%d value=%x\n  distinct outputs: %v",
		scenarioName, protocolName,
		report,
		o.Decided, o.DecidedRound, o.DecidedValue,
		report.DistinctOutputs,
	))
}
