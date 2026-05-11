package consensustest

import (
	"errors"
	"sort"
	"testing"
)

// expectKeys returns the sorted list of protocol names a scenario declares
// expectations for; used for diagnostic messages when a scenario is missing
// a coverage entry for a protocol the test is running.
func expectKeys(m map[string]ExpectClass) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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
	Why          string // mismatch rationale; empty on Match=true
	Skipped      bool   // true when adapter returned ErrNotApplicable
}

// RunScenarioOnProtocol applies `s` to `base`, runs `p`, computes safety
// invariants, classifies outcome vs expectation, and returns a Result.
// Any SafetyReport.IsViolation() panics (Agreement, QuorumBackedDecision,
// NoEquivocationAccepted, and the OBFT-specific commit-kind /
// host-validity checks); non-safety mismatches are recorded in
// Result.Match for the caller to assert. Does not call t.Fatal.
func RunScenarioOnProtocol(t *testing.T, p Protocol, s Scenario, base SimConfig) Result {
	t.Helper()
	cfg := base
	if s.Apply != nil {
		s.Apply(&cfg)
	}

	expect, declared := s.Expect[p.Name()]
	if !declared {
		t.Fatalf("scenario %q has no Expect entry for protocol %q (Expect map keys: %v)",
			s.Name, p.Name(), expectKeys(s.Expect))
	}
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
		// The matching ExpectNotApplicable case already returned above, so
		// reaching here means the scenario expected an outcome but the
		// adapter declined to translate it — record a mismatch.
		return Result{
			ProtocolName: p.Name(),
			ScenarioName: s.Name,
			Expected:     expect,
			Match:        false,
			Why:          "adapter returned ErrNotApplicable but scenario expected " + expect.String(),
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
	if safety.IsViolation() {
		SafetyPanic(safety, s.Name, p.Name(), expect, out)
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
