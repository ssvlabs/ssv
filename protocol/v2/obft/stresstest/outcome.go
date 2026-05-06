package stresstest

import (
	"fmt"
	"strings"
	"time"

	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Outcome captures the result of one simulation. The PerOp map records
// each operator's individual decision so tests can assert "all honest
// converged on the same value" or "byz operator missed".
type Outcome struct {
	Decided      bool                  // any operator reached a decision
	Layer        int                   // earliest-decider's layer; -1 if none
	Value        obft.Value            // earliest-decider's value
	DecisionTime time.Duration         // earliest decision time
	PerOp        map[obft.OperatorID]OperatorOutcome
	Evidence     map[obft.EvidenceRule]int
	Trace        []TraceEntry
}

// OperatorOutcome is one operator's view of the result.
type OperatorOutcome struct {
	Decided       bool
	Layer         int
	Value         obft.Value
	Time          time.Duration
	EvidenceCount int
	Err           string
}

// TraceEntry records one event handled by the loop.
type TraceEntry struct {
	When  time.Duration
	Event string
}

// String returns a compact one-line summary for failure diagnostics.
func (o Outcome) String() string {
	if !o.Decided {
		return fmt.Sprintf("MISS (no operator decided; evidence=%v)", o.Evidence)
	}
	return fmt.Sprintf("DECIDED layer=%d t=%v value=%s evidence=%v",
		o.Layer, o.DecisionTime, hashValue(o.Value), o.Evidence)
}

// AllAgree reports whether every operator that decided agrees on the
// (Layer, Value) pair. Useful as a single safety-property assertion.
func (o Outcome) AllAgree() bool {
	var ref *OperatorOutcome
	for op := range o.PerOp {
		oo := o.PerOp[op]
		if !oo.Decided {
			continue
		}
		if ref == nil {
			oo := oo
			ref = &oo
			continue
		}
		if oo.Layer != ref.Layer && ref.Layer != -1 && oo.Layer != -1 {
			return false
		}
		if string(oo.Value) != string(ref.Value) {
			return false
		}
	}
	return true
}

// QuorumDecided reports how many operators reached a successful decision.
func (o Outcome) QuorumDecided() int {
	n := 0
	for _, oo := range o.PerOp {
		if oo.Decided {
			n++
		}
	}
	return n
}

// FormatTrace returns the event trace as a multi-line string for failure
// dumps. Empty when TraceEnabled was off in the run config.
func (o Outcome) FormatTrace() string {
	if len(o.Trace) == 0 {
		return "(trace not recorded; set SimConfig.TraceEnabled=true)"
	}
	var b strings.Builder
	for _, t := range o.Trace {
		fmt.Fprintf(&b, "  %v %s\n", t.When, t.Event)
	}
	return b.String()
}

// ---- Expected outcomes for canonical scenarios -------------------------

// ExpectClass is the expected outcome bucket for a (scenario, byz, host)
// combination, as predicted by docs/BFT-comparison.md.
type ExpectClass int

const (
	// ExpectSuccessHealthy: σ-quorum at L_0 within budget; all honest
	// agree on the canonical L_0 value.
	ExpectSuccessHealthy ExpectClass = iota

	// ExpectSuccessFallThrough: σ-quorum at some layer > 0 (silent leader
	// or all-Defer recovery); all honest agree.
	ExpectSuccessFallThrough

	// ExpectMiss: no σ-quorum at any layer; slot misses cleanly. No
	// safety violation expected.
	ExpectMiss

	// ExpectMissOrSucceedAtLater: equivocation outcomes where the
	// adversary's timing controls which sub-class fires. Treated as a
	// "either succeeds at L≥1 or misses; neither is a bug".
	ExpectMissOrSucceedAtLater
)

func (e ExpectClass) String() string {
	switch e {
	case ExpectSuccessHealthy:
		return "success/healthy(L_0)"
	case ExpectSuccessFallThrough:
		return "success/fall-through(L>=1)"
	case ExpectMiss:
		return "miss"
	case ExpectMissOrSucceedAtLater:
		return "miss-or-fall-through"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// CheckExpectation reports whether `o` matches `expect`, plus a short
// rationale on mismatch.
func CheckExpectation(o Outcome, expect ExpectClass) (ok bool, why string) {
	switch expect {
	case ExpectSuccessHealthy:
		if !o.Decided {
			return false, fmt.Sprintf("expected success at L_0; got MISS (%s)", o)
		}
		if o.Layer != 0 {
			return false, fmt.Sprintf("expected L_0; got L_%d", o.Layer)
		}
		if !o.AllAgree() {
			return false, "operators disagreed on (layer, value)"
		}
		return true, ""

	case ExpectSuccessFallThrough:
		if !o.Decided {
			return false, fmt.Sprintf("expected success at L>=1; got MISS (%s)", o)
		}
		if o.Layer == 0 {
			return false, fmt.Sprintf("expected L>=1 (fall-through); got L_0")
		}
		if !o.AllAgree() {
			return false, "operators disagreed on (layer, value)"
		}
		return true, ""

	case ExpectMiss:
		if o.Decided {
			return false, fmt.Sprintf("expected MISS; got %s", o)
		}
		return true, ""

	case ExpectMissOrSucceedAtLater:
		if !o.Decided {
			return true, ""
		}
		if o.Layer >= 1 && o.AllAgree() {
			return true, ""
		}
		return false, fmt.Sprintf("expected MISS or success at L>=1; got %s", o)

	default:
		return false, fmt.Sprintf("unknown expect class %d", int(expect))
	}
}
