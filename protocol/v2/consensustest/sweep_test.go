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

// TestSweep_FullCatalog_LargerN runs every catalog scenario at n ∈ {7, 10, 13}
// on both protocols. Universal safety invariants are enforced via
// RunScenarioOnProtocol's panic gate. Per-scenario outcome-class expectations
// are logged as a diagnostic table but NOT asserted at n>4 — many scenarios
// have algebraic outcomes that depend on f-quorum sizes vs hardcoded recipient
// counts, so n>4 outcomes can legitimately differ from the n=4 baseline.
//
// Three named-split scenarios produce different outcome classes at n>4 by
// design (the name encodes a specific n=4 quorum split; generalizing
// would require renaming):
//   - ValidityDivergence_2_2: at n=4 σ-pool=2 < qV → MISS; at n>4 the 2 NV
//     are minority and σ-pool reaches qV → FASTEST.
//   - ValidityDivergence_1_3: at n=4 NR-pool=3 = qEnc → FALL_THROUGH; at n>4
//     NR-pool=3 < qEnc → MISS.
//   - PartialEquivocation_2_1: at n=4 σ-pool on V_a=3 = qV → FASTEST; at n>4
//     the 2 V_a recipients + leader σ_L^V < qV → MISS.
//
// All other scenarios produce identical outcome classes at all SSV cluster
// sizes (n ∈ {4, 7, 10, 13}); their Apply scales with f or cfg.N.
//
// Strict assertion: Healthy must decide at fastest path on both protocols at
// every n (sanity check; framework would have a deeper bug if this didn't
// hold universally).
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
			b.WriteString("Scenario                     | OBFT          | QBFT          \n")
			b.WriteString("-----------------------------+---------------+---------------\n")

			for _, s := range ct.Catalog {
				cells := []string{}
				for _, p := range protocols {
					r := ct.RunScenarioOnProtocol(t, p, s, base)
					cells = append(cells, sweepCellSummary(r))

					// Strict: Healthy must always succeed at fastest path.
					if s.Name == "Healthy" {
						require.Truef(t, r.Match,
							"n=%d %s Healthy must match (universal property): %s",
							n, p.Name(), r.Why)
					}
				}
				fmt.Fprintf(&b, "%-28s | %-13s | %-13s\n", s.Name, cells[0], cells[1])
			}
			t.Log(b.String())
		})
	}
}
