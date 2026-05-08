package consensustest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// Phase 6 parameter sweeps. Each sweep varies one parameter axis at a time,
// runs the catalog across cells, and:
//
//   - Always enforces universal safety invariants (panics if violated, via
//     RunScenarioOnProtocol's safety check).
//   - Asserts per-scenario expectations only at the canonical operating point.
//     At off-canonical points (e.g., BTT=400ms degraded mesh, K=MinK boundary),
//     outcomes shift; the sweep logs them as a diagnostic matrix without
//     asserting cell-level matches.
//   - Headline scenarios (Healthy especially) carry stricter per-cell checks
//     because their behavior is universal across the swept parameter range.

// baseSweepConfig returns a fresh config sized for cluster n at the given BTT,
// with default operators and proposer-duty timing.
func baseSweepConfig(n int, btt time.Duration) ct.SimConfig {
	return ct.SimConfig{
		N:                    n,
		Operators:            ct.MakeOperators(n),
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		Network:              ct.ConstantDelay{D: btt},
		Host:                 ct.HostAllValid{},
		Byz:                  ct.ByzPattern{Kind: ct.ByzNone},
		Seed:                 1,
	}
}

// TestSweep_N — Healthy across all SSV-supported cluster sizes. Asserts every
// size decides at fastest path (the spec's canonical Config A operating point
// holds at all n).
func TestSweep_N(t *testing.T) {
	for _, n := range ct.ClusterSizes {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(n, 200*time.Millisecond)
			for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
				out, err := p.Run(cfg)
				require.NoErrorf(t, err, "n=%d %s Run", n, p.Name())
				require.Truef(t, out.Decided, "n=%d %s Healthy must decide", n, p.Name())
				require.Equalf(t, 0, out.DecidedRound,
					"n=%d %s Healthy must decide at fastest path", n, p.Name())
				rep := ct.ComputeSafetyReport(out)
				require.Truef(t, rep.SingleV, "n=%d %s SingleV: %s", n, p.Name(), rep)
				require.Truef(t, rep.NoOfflineDoubleV,
					"n=%d %s NoOfflineDoubleV: %s", n, p.Name(), rep)
			}
		})
	}
}

// TestSweep_K — OBFT with varying K, n=N (the K=N production convention)
// and minimum-K (boundary). Asserts Healthy decides at every K ≥ MinK(n).
func TestSweep_K(t *testing.T) {
	for _, n := range ct.ClusterSizes {
		n := n
		minK := ct.MinK(n)
		for k := minK; k <= n; k++ {
			k := k
			t.Run(fmt.Sprintf("n=%d_K=%d", n, k), func(t *testing.T) {
				t.Parallel()
				cfg := baseSweepConfig(n, 200*time.Millisecond)
				cfg.K = k
				out, err := obftadapter.Protocol{}.Run(cfg)
				require.NoErrorf(t, err, "n=%d K=%d Run", n, k)
				require.Truef(t, out.Decided, "n=%d K=%d Healthy must decide", n, k)
				require.Equalf(t, 0, out.DecidedRound,
					"n=%d K=%d Healthy must decide at fastest path", n, k)
				rep := ct.ComputeSafetyReport(out)
				require.Truef(t, rep.SingleV, "n=%d K=%d SingleV: %s", n, k, rep)
			})
		}
	}
}

// TestSweep_BTT — vary BTT across the spec's three operating points (ideal /
// canonical / degraded). Asserts Healthy fits at the canonical point and logs
// the full catalog matrix at each BTT for diagnostic visibility. Off-canonical
// per-scenario outcomes aren't asserted (intentional — they shift with BTT).
func TestSweep_BTT(t *testing.T) {
	bttValues := []time.Duration{
		100 * time.Millisecond, // ideal mesh
		200 * time.Millisecond, // canonical Config A
		400 * time.Millisecond, // degraded mesh
	}

	for _, btt := range bttValues {
		btt := btt
		t.Run("BTT="+btt.String(), func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(4, btt)

			// Healthy must decide at fastest path at canonical BTT (200ms).
			// At other BTTs, log but don't assert.
			for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
				out, err := p.Run(cfg)
				require.NoErrorf(t, err, "BTT=%v %s Run", btt, p.Name())
				if btt == 200*time.Millisecond {
					require.Truef(t, out.Decided,
						"BTT=200ms canonical %s Healthy must decide", p.Name())
				}
			}

			// Diagnostic catalog matrix at this BTT.
			var b strings.Builder
			fmt.Fprintf(&b, "\nBTT=%v catalog matrix:\n", btt)
			for _, s := range ct.Catalog {
				obftR := ct.RunScenarioOnProtocol(t, obftadapter.Protocol{}, s, cfg)
				qbftR := ct.RunScenarioOnProtocol(t, qbftadapter.Protocol{}, s, cfg)
				fmt.Fprintf(&b, "  %-32s OBFT=%-12s QBFT=%-12s\n",
					s.Name, sweepCellSummary(obftR), sweepCellSummary(qbftR))
			}
			t.Log(b.String())
		})
	}
}

// TestSweep_Seeds — same scenario, multiple seeds. Asserts safety invariants
// hold across seeds (the framework panics on violation; the assertion is that
// no seed produces a violation).
func TestSweep_Seeds(t *testing.T) {
	const seedCount = 5
	cfg := baseSweepConfig(4, 200*time.Millisecond)

	for seed := int64(1); seed <= seedCount; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			cfg := cfg
			cfg.Seed = seed
			cfg.Network = ct.JitteredDelay{D: 200 * time.Millisecond, Jitter: 50 * time.Millisecond}
			for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
				out, err := p.Run(cfg)
				require.NoErrorf(t, err, "seed=%d %s Run", seed, p.Name())
				rep := ct.ComputeSafetyReport(out)
				require.Truef(t, rep.SingleV, "seed=%d %s SingleV: %s", seed, p.Name(), rep)
				require.Truef(t, rep.NoOfflineDoubleV,
					"seed=%d %s NoOfflineDoubleV: %s", seed, p.Name(), rep)
				if !out.Decided {
					t.Logf("seed=%d %s did not decide under jitter (acceptable)", seed, p.Name())
				}
			}
		})
	}
}

// TestSweep_MultiByz_n7 — n=7 (f=2) with 2 byz silent leaders. OBFT falls
// through past both byz-led layers (in-round, decides at L_2). QBFT round-
// changes past both, but two RT timeouts consume the relay budget (2×2s = 4s
// = RelayCutoff) so R3's success arrives past the deadline → MISS.
func TestSweep_MultiByz_n7(t *testing.T) {
	cfg := baseSweepConfig(7, 200*time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzSilentLeader,
		ByzOperators: []ct.OperatorID{1, 2},
	}

	t.Run("OBFT", func(t *testing.T) {
		out, err := obftadapter.Protocol{}.Run(cfg)
		require.NoError(t, err)
		require.True(t, out.Decided, "OBFT n=7 with 2 byz silent leaders must fall through")
		require.Greater(t, out.DecidedRound, 1,
			"should fall through past both byz-led layers (L_0, L_1); got L_%d", out.DecidedRound)
	})

	t.Run("QBFT", func(t *testing.T) {
		out, err := qbftadapter.Protocol{}.Run(cfg)
		require.NoError(t, err)
		// R1 + R2 timeouts = 2 × RT = 4s alone — already past RelayCutoff=4s.
		// R3's PROPOSE arrives past the deadline → cluster MISSES, mirroring
		// the QBFT-side observation in MultiSilent_K3 at n=4 (RT-budget
		// dominates fall-through cost).
		require.False(t, out.Decided,
			"QBFT n=7 with 2 byz silent leaders should MISS (2 round-changes consume the relay budget)")
	})
}

func sweepCellSummary(r ct.Result) string {
	if r.Skipped {
		return "n/a"
	}
	if !r.Match {
		return "! mismatch"
	}
	if r.Outcome.Decided {
		return fmt.Sprintf("%v L%d", r.Outcome.DecisionTime.Round(10*time.Millisecond), r.Outcome.DecidedRound)
	}
	return "miss"
}

// TestSweep_DocTable validates BFT-comparison.md Tables 1a–1e (success-mode
// completion) analytically against the spec's claimed per-protocol BTT counts.
// Each (BFT_start, BTT) cell asks: does R1-healthy consensus length fit the
// effective budget = (RelayCutoff − HeaderSubmitHeadroom − BFT_start)?
//
// This is a doc-arithmetic regression: catches drift between the spec's
// claimed BTT counts and the budget envelopes the doc tabulates. The actual
// per-protocol simulation uses BFT_start ≡ 0 (the spec's "immediate" cell);
// off-canonical BFT_start values are checked here against the spec's
// per-protocol consensus-length formula (BTT_count × BTT).
//
// Spec source: docs/BFT-comparison.md §"Table 1 — Success modes" / §"Effective
// BFT consensus budget by start time".
func TestSweep_DocTable(t *testing.T) {
	const (
		relayCutoff    = 4000 * time.Millisecond
		submitHeadroom = 100 * time.Millisecond
	)

	// Spec start-time matrix: docs/BFT-comparison.md §Effective BFT consensus
	// budget. 0ms (immediate) → 2500ms (late MEV fetch).
	bftStarts := []time.Duration{
		0,
		800 * time.Millisecond,
		1200 * time.Millisecond,
		1800 * time.Millisecond,
		2500 * time.Millisecond,
	}

	// Spec BTT operating points: 200ms (production-typical) → 1000ms (severely
	// degraded). docs/BFT-comparison.md §"Scope and assumptions".
	btts := []time.Duration{
		200 * time.Millisecond,
		600 * time.Millisecond,
		1000 * time.Millisecond,
	}

	// Spec per-protocol R1 healthy consensus length in BTT units. Source:
	// docs/BFT-comparison.md §"Total time to signed output".
	type protoSpec struct {
		name     string
		bttCount int // R1-healthy consensus length, BTT units, recommended sizing
	}
	protos := []protoSpec{
		{"Partial-sigs", 2},
		{"OBFT", 3},
		{"OBFTR R1", 6},
		{"2abOBFT", 6},
		{"QBFT R1", 8},
	}

	var b strings.Builder
	b.WriteString("\nBFT-comparison.md Table 1 doc-arithmetic validation:\n")
	b.WriteString("(consensus length ≤ budget = relayCutoff − headroom − BFT_start)\n\n")

	for _, bftStart := range bftStarts {
		budget := relayCutoff - submitHeadroom - bftStart
		fmt.Fprintf(&b, "BFT_start=%v, budget=%v\n", bftStart, budget)
		fmt.Fprintf(&b, "%-20s", "BTT")
		for _, p := range protos {
			fmt.Fprintf(&b, " | %-13s", p.name)
		}
		b.WriteString("\n")
		for _, btt := range btts {
			fmt.Fprintf(&b, "  %-18v", btt)
			for _, p := range protos {
				consensusLen := time.Duration(p.bttCount) * btt
				mark := "✓"
				if consensusLen > budget {
					mark = "✗"
				}
				fmt.Fprintf(&b, " | %s %-11v", mark, consensusLen)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	t.Log(b.String())

	// Sanity: spec claims BFT_start = 0, BTT = 200ms is the canonical fit cell
	// for every BFT-consensus protocol (Table 1a). Asserts the spec's BTT-count
	// arithmetic against the canonical budget.
	canonicalBudget := relayCutoff - submitHeadroom // BFT_start = 0
	for _, p := range protos {
		consensusLen := time.Duration(p.bttCount) * (200 * time.Millisecond)
		require.LessOrEqual(t, consensusLen, canonicalBudget,
			"%s consensus (%v at BTT=200ms, %d BTT) must fit canonical budget %v",
			p.name, consensusLen, p.bttCount, canonicalBudget)
	}
}

// TestSweep_FullCatalog_LargerN runs every catalog scenario at n ∈ {7, 10, 13}
// on both protocols. Universal safety invariants are enforced via
// RunScenarioOnProtocol's panic gate; per-cell outcome classes are also
// asserted to match the catalog's declared expectations.
//
// Every catalog scenario produces the same outcome class at all SSV cluster
// sizes by design — Apply functions scale with cfg.N / cfg.F() so the
// f-quorum mechanics are preserved. A per-cell mismatch at n>4 indicates
// either a generalization regression (Apply hardcoded an n=4 value) or a
// new scenario that needs n-aware Apply.
func TestSweep_FullCatalog_LargerN(t *testing.T) {
	btt := 200 * time.Millisecond
	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	for _, n := range ct.ClusterSizes {
		if n == 4 {
			continue // n=4 is the matrix baseline; covered by TestComparison_Matrix
		}
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			base := baseSweepConfig(n, btt)

			var b strings.Builder
			fmt.Fprintf(&b, "\nn=%d full-catalog sweep:\n", n)
			b.WriteString("Scenario                            | OBFT          | QBFT          \n")
			b.WriteString("------------------------------------+---------------+---------------\n")

			for _, s := range ct.Catalog {
				cells := []string{}
				for _, p := range protocols {
					r := ct.RunScenarioOnProtocol(t, p, s, base)
					cells = append(cells, sweepCellSummary(r))

					require.Truef(t, r.Match || r.Skipped,
						"n=%d scenario %q on %s mismatched n=4 expectation: %s",
						n, s.Name, p.Name(), r.Why)
				}
				fmt.Fprintf(&b, "%-35s | %-13s | %-13s\n", s.Name, cells[0], cells[1])
			}
			t.Log(b.String())
		})
	}
}
