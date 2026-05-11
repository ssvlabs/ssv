package consensustest

import "fmt"

// Scenario describes a test condition independently of the protocol under
// test. Apply modifies SimConfig (typically Byz / Host / Network); Expect
// declares per-protocol outcome buckets.
type Scenario struct {
	Name   string
	Title  string // human-readable label for reports/charts; falls back to Name when empty
	Apply  func(*SimConfig)
	Expect map[string]ExpectClass // keyed by Protocol.Name()
	Note   string                 // doc pointer (BFT-comparison.md row, OBFT.md section, ...)
}

// DisplayTitle returns the scenario's Title if set, otherwise Name. Used
// by report renderers for human-readable chart/section labels.
func (s Scenario) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}

// ExpectClass enumerates the canonical outcome buckets.
type ExpectClass int

const (
	ExpectSuccessFastest     ExpectClass = iota // round/layer 0 (Healthy)
	ExpectSuccessFallThrough                    // deeper round/layer
	ExpectMiss                                  // clean slot miss, no safety violation
	ExpectNotApplicable                         // scenario doesn't translate to this protocol
	ExpectSuccessOrMiss                         // outcome timing-dependent; either accepted
)

func (e ExpectClass) String() string {
	switch e {
	case ExpectSuccessFastest:
		return "success/fastest"
	case ExpectSuccessFallThrough:
		return "success/fall-through"
	case ExpectMiss:
		return "miss"
	case ExpectNotApplicable:
		return "n/a"
	case ExpectSuccessOrMiss:
		return "success-or-miss"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// Match reports whether outcome `o` matches `expect`, plus a rationale on
// mismatch. Reaching the ExpectNotApplicable case here means the adapter ran
// despite the scenario being declared n/a — a translation bug in the adapter.
func Match(o Outcome, expect ExpectClass) (ok bool, why string) {
	switch expect {
	case ExpectSuccessFastest:
		if !o.Decided {
			return false, "expected success at fastest path; got MISS"
		}
		if o.DecidedRound != 0 {
			return false, fmt.Sprintf("expected fastest path (round/layer 0); got %d", o.DecidedRound)
		}
		return true, ""
	case ExpectSuccessFallThrough:
		if !o.Decided {
			return false, "expected success via fall-through; got MISS"
		}
		if o.DecidedRound == 0 {
			return false, "expected deeper round/layer (>=1); got 0"
		}
		return true, ""
	case ExpectMiss:
		if o.Decided {
			return false, fmt.Sprintf("expected MISS; got success at round %d", o.DecidedRound)
		}
		return true, ""
	case ExpectNotApplicable:
		return false, "protocol returned an Outcome but scenario was declared not applicable"
	case ExpectSuccessOrMiss:
		return true, ""
	default:
		return false, fmt.Sprintf("unknown expect class %d", int(expect))
	}
}
