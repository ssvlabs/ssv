package consensustest

import (
	"errors"
	"testing"
)

// Result bundles an Outcome with the universal safety report and the
// expectation match. RunScenarioOnProtocol returns one of these per
// (protocol, scenario) cell.
type Result struct {
	ProtocolName string
	ScenarioName string
	Outcome      Outcome
	Safety       SafetyReport
	Expected     ExpectClass
	Match        bool
	Why          string  // mismatch rationale; empty on Match=true
	Skipped      bool    // true when adapter returned ErrNotApplicable
}

// RunScenarioOnProtocol applies `s` to `base`, runs `p`, computes safety
// invariants, classifies outcome vs expectation, and returns a Result.
// SingleV / HonestAgreement violations panic; non-safety mismatches are
// recorded in Result.Match for the caller to assert. Does not call t.Fatal.
func RunScenarioOnProtocol(t *testing.T, p Protocol, s Scenario, base SimConfig) Result {
	t.Helper()
	cfg := base
	if s.Apply != nil {
		s.Apply(&cfg)
	}

	expect := s.Expect[p.Name()]
	if expect == ExpectNotApplicable {
		return Result{
			ProtocolName: p.Name(),
			ScenarioName: s.Name,
			Expected:     expect,
			Match:        true,
			Skipped:      true,
		}
	}

	out, err := p.Run(cfg)
	if errors.Is(err, ErrNotApplicable) {
		// Reconcile adapter and scenario: if both say not-applicable, match.
		ok := expect == ExpectNotApplicable
		why := ""
		if !ok {
			why = "adapter returned ErrNotApplicable but scenario expected " + expect.String()
		}
		return Result{
			ProtocolName: p.Name(),
			ScenarioName: s.Name,
			Expected:     expect,
			Match:        ok,
			Why:          why,
			Skipped:      true,
		}
	}
	if err != nil {
		t.Logf("adapter %s scenario %s: Run error: %v", p.Name(), s.Name, err)
		return Result{
			ProtocolName: p.Name(),
			ScenarioName: s.Name,
			Expected:     expect,
			Match:        false,
			Why:          "adapter error: " + err.Error(),
		}
	}

	safety := ComputeSafetyReport(out)
	if !safety.SingleV || !safety.HonestAgreement {
		SafetyPanic(safety, s.Name, p.Name(), out)
	}
	if !safety.Terminated {
		// Adapters shouldn't produce this state in normal operation; warn
		// so the test surfaces it without crashing the matrix run.
		t.Logf("WARN: %s/%s: protocol did not terminate cleanly\n  %s",
			p.Name(), s.Name, safety)
	}

	match, why := Match(out, expect)
	return Result{
		ProtocolName: p.Name(),
		ScenarioName: s.Name,
		Outcome:      out,
		Safety:       safety,
		Expected:     expect,
		Match:        match,
		Why:          why,
	}
}
