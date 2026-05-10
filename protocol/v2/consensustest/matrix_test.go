package consensustest_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestCatalog_NoUnsafeByzKinds guards against accidentally adding a negative-
// test byz kind (one that deliberately produces a NoOfflineDoubleV violation)
// to the catalog. RunScenarioOnProtocol panics on safety violations, so a
// catalog scenario using these kinds would crash every matrix-style test.
// Better to fail fast and pointed here than to debug a SafetyPanic stack.
func TestCatalog_NoUnsafeByzKinds(t *testing.T) {
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	unsafeKinds := map[ct.ByzKind]string{
		ct.ByzAggregatorBypass: "deliberately produces NoOfflineDoubleV via forged-identity Layers[0]",
		ct.ByzWitnessForgery:   "deliberately produces NoOfflineDoubleV via forged Witnesses[]",
	}
	for _, s := range ct.Catalog {
		cfg := base
		if s.Apply != nil {
			s.Apply(&cfg)
		}
		if reason, unsafe := unsafeKinds[cfg.Byz.Kind]; unsafe {
			t.Fatalf("scenario %q uses byz kind %v which %s — these MUST stay out of Catalog (matrix tests would crash on SafetyPanic). Remove from Catalog and exercise via a standalone test instead.",
				s.Name, cfg.Byz.Kind, reason)
		}
	}
}

// TestCatalog_AllScenariosGeneralized verifies every catalog scenario's
// Apply function scales correctly across SSV cluster sizes. Catches the
// "Apply hardcoded operator 4 in a function meant to scale with cfg.F()"
// class of regression at scenario-add time, before it surfaces as a
// TestSweep_FullCatalog_LargerN mismatch at n>4.
//
// Per cluster size n ∈ ClusterSizes, runs Apply on a fresh base config
// and verifies:
//   - Apply doesn't panic.
//   - Resulting Byz.ByzOperators is within the f-bound (len ≤ F).
//   - Every operator ID in ByzOperators / Recipients is in [1, N].
//
// Does NOT run the protocol — purely a structural check on the
// post-Apply SimConfig. Cheap (no DES execution); runs in <50ms total.
func TestCatalog_AllScenariosGeneralized(t *testing.T) {
	for _, n := range ct.ClusterSizes {
		n := n
		for _, s := range ct.Catalog {
			s := s
			t.Run(fmt.Sprintf("n=%d/%s", n, s.Name), func(t *testing.T) {
				base := ct.SimConfig{
					N:                    n,
					Operators:            ct.MakeOperators(n),
					SlotDuration:         12 * time.Second,
					RelayCutoff:          4 * time.Second,
					HeaderSubmitHeadroom: 100 * time.Millisecond,
					BTT:                  200 * time.Millisecond,
					Host:                 ct.HostAllValid{},
					Byz:                  ct.ByzPattern{Kind: ct.ByzNone},
					Seed:                 1,
				}
				// Apply may panic on a malformed scenario; let the test framework
				// surface it as a test failure (subtest fails, others continue).
				if s.Apply != nil {
					s.Apply(&base)
				}

				f := base.F()
				require.LessOrEqualf(t, len(base.Byz.ByzOperators), f,
					"n=%d %q: ByzOperators length %d exceeds f=%d (Apply not scaling with cfg.F()?)",
					n, s.Name, len(base.Byz.ByzOperators), f)
				for _, op := range base.Byz.ByzOperators {
					require.GreaterOrEqualf(t, int(op), 1,
						"n=%d %q: ByzOperators contains op=%d < 1", n, s.Name, op)
					require.LessOrEqualf(t, int(op), n,
						"n=%d %q: ByzOperators contains op=%d > N=%d (hardcoded n=4 value?)",
						n, s.Name, op, n)
				}
				for _, op := range base.Byz.Recipients {
					require.GreaterOrEqualf(t, int(op), 1,
						"n=%d %q: Recipients contains op=%d < 1", n, s.Name, op)
					require.LessOrEqualf(t, int(op), n,
						"n=%d %q: Recipients contains op=%d > N=%d (hardcoded n=4 value?)",
						n, s.Name, op, n)
				}
			})
		}
	}
}

// TestComparison_Matrix runs every scenario × protocol combination and
// asserts the outcome matches the scenario's declared per-protocol
// expectation. Universal safety invariants (no two V signatures, honest
// agreement, termination) are enforced by RunScenarioOnProtocol — any
// violation panics regardless of declared expectation.
//
// Output: a compact ASCII table of the comparison matrix, plus a final
// pass/fail signal based on whether any cell mismatched.
func TestComparison_Matrix(t *testing.T) {
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)

	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		qbftadapter.Protocol{},
	}
	protoNames := []string{}
	for _, p := range protocols {
		protoNames = append(protoNames, p.Name())
	}

	scenarioNames := []string{}
	for _, s := range ct.Catalog {
		scenarioNames = append(scenarioNames, s.Name)
	}

	matrix := ct.NewMatrixReport(protoNames, scenarioNames)
	for _, p := range protocols {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, base)
			matrix.Record(r)
		}
	}

	report := matrix.Render()
	t.Logf("\n%s", report)

	if matrix.AnyMismatch() {
		t.Fatalf("comparison matrix has mismatches:\n%s", report)
	}
}

// TestComparison_BTTSweep runs the matrix at multiple BTT operating points
// to surface where each protocol's envelope breaks. Logs a per-BTT table;
// safety invariants are enforced inside RunScenarioOnProtocol via panic, so
// any SingleV/HonestAgreement violation crashes the run. Per-cell match
// expectations are intentionally NOT asserted — at off-spec BTT values, some
// scenarios are expected to mismatch (that's the sweep's purpose).
func TestComparison_BTTSweep(t *testing.T) {
	bttValues := []time.Duration{
		100 * time.Millisecond, // ideal mesh
		200 * time.Millisecond, // operating point per OBFT.md §Application
		400 * time.Millisecond, // degraded mesh
	}

	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		qbftadapter.Protocol{},
	}

	for _, btt := range bttValues {
		btt := btt
		t.Run("BTT="+btt.String(), func(t *testing.T) {
			t.Parallel()
			base := ct.DefaultProposerDutyConfig(btt)

			var b strings.Builder
			b.WriteString("\nBTT=" + btt.String() + ":\n")
			b.WriteString("Scenario                    | OBFT          | QBFT          | Notes\n")
			b.WriteString("----------------------------+---------------+---------------+----------------\n")
			for _, s := range ct.Catalog {
				cells := []string{}
				for _, p := range protocols {
					r := ct.RunScenarioOnProtocol(t, p, s, base)
					cell := renderCell(r)
					cells = append(cells, cell)
				}
				b.WriteString(formatRow(s.Name, cells, s.Note))
			}
			t.Log(b.String())
		})
	}
}

func renderCell(r ct.Result) string {
	if r.Skipped {
		return "n/a"
	}
	if !r.Match {
		return "! mismatch"
	}
	if r.Outcome.Decided {
		return durationOnly(r.Outcome.DecisionTime) + " L" + intStr(r.Outcome.DecidedRound)
	}
	return "✗ miss"
}

func formatRow(name string, cells []string, note string) string {
	// 35 cols accommodate the longest current scenario name
	// (PartialEquivocation_NaturalRecovery = 34).
	out := padRight(name, 35) + " |"
	for _, c := range cells {
		out += " " + padRight(c, 13) + " |"
	}
	if note != "" && len(note) > 60 {
		note = note[:60] + "..."
	}
	out += " " + note + "\n"
	return out
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func intStr(i int) string {
	return strconv.Itoa(i)
}

func durationOnly(d time.Duration) string {
	rounded := d.Round(10 * time.Millisecond)
	return rounded.String()
}
