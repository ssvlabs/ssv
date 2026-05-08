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

// TestSweep_Jitter — vary JitteredDelay jitter across the catalog at canonical
// BTT=200ms. Verifies liveness holds across propagation jitter (one of the
// "(byz, network, clock)" axes the spec's partial-synchrony bound covers —
// see CONSENSUS-TEST-PLAN.md / Tier 1 stress-test additions). Safety is
// enforced via RunScenarioOnProtocol's panic gate; per-cell outcomes are
// logged for diagnostic visibility (decision-rate gradient as jitter widens)
// without per-cell asserts because high-jitter shifts outcomes legitimately.
//
// Jitter levels:
//   - 0ms: deterministic baseline (matches ConstantDelay; sanity-check that
//     Healthy decides under both protocols)
//   - 50ms: production-typical (matches TestSweep_Seeds + TestSmoke_JitteredNetwork)
//   - 100ms: stressed (jitter half of BTT)
//   - 200ms: pathological (jitter = BTT — propagation can drop to ~0 or 2x)
//
// At ≥100ms jitter some catalog cells legitimately miss (tight scenarios where
// the late tail exceeds B_k absorption). The test asserts safety holds, not
// that every cell decides.
func TestSweep_Jitter(t *testing.T) {
	jitterLevels := []time.Duration{
		0,
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
	}

	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	for _, jitter := range jitterLevels {
		jitter := jitter
		t.Run(fmt.Sprintf("jitter=%v", jitter), func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(4, 200*time.Millisecond)
			cfg.Network = ct.JitteredDelay{D: cfg.BTT, Jitter: jitter}

			// Sanity: at jitter=0 (≡ ConstantDelay baseline), Healthy must
			// decide for both protocols. Validates the JitteredDelay model is
			// correctly wired and matches default-config behavior.
			if jitter == 0 {
				for _, p := range protocols {
					out, err := p.Run(cfg)
					require.NoErrorf(t, err, "jitter=%v %s Run", jitter, p.Name())
					require.Truef(t, out.Decided,
						"jitter=0ms %s Healthy must decide (matches ConstantDelay baseline)", p.Name())
				}
			}

			// Diagnostic catalog matrix — RunScenarioOnProtocol panics on any
			// safety violation, so just running every scenario provides the
			// safety check. Decision-rate denominators count non-skipped cells
			// (some catalog scenarios are OBFT-specific and skip on QBFT).
			var b strings.Builder
			fmt.Fprintf(&b, "\njitter=%v catalog matrix:\n", jitter)
			obftTotal, qbftTotal := 0, 0
			obftDecided, qbftDecided := 0, 0
			for _, s := range ct.Catalog {
				obftR := ct.RunScenarioOnProtocol(t, obftadapter.Protocol{}, s, cfg)
				qbftR := ct.RunScenarioOnProtocol(t, qbftadapter.Protocol{}, s, cfg)
				if !obftR.Skipped {
					obftTotal++
					if obftR.Outcome.Decided {
						obftDecided++
					}
				}
				if !qbftR.Skipped {
					qbftTotal++
					if qbftR.Outcome.Decided {
						qbftDecided++
					}
				}
				fmt.Fprintf(&b, "  %-32s OBFT=%-12s QBFT=%-12s\n",
					s.Name, sweepCellSummary(obftR), sweepCellSummary(qbftR))
			}
			fmt.Fprintf(&b, "decision rate: OBFT=%d/%d QBFT=%d/%d\n",
				obftDecided, obftTotal, qbftDecided, qbftTotal)
			t.Log(b.String())
		})
	}
}

// TestSweep_Asymmetric — vary the count of honest operators that see
// elevated propagation delay (2× BTT) via PerReceiverDelay overrides on
// top of a ConstantDelay base. Verifies per-layer absorption-window
// semantics under realistic mesh asymmetry — when ≤ f honest are slow,
// OBFT's staggered design must still decide via the in-time honest
// majority; when > f honest are slow, fall-through behavior shifts but
// safety must hold. Tier 1 stress-test addition per CONSENSUS-TEST-PLAN.md.
//
// Slow-receiver counts:
//   - 0: baseline (matches ConstantDelay)
//   - 1: within OBFT's f-bound at n=4 (1 honest slow → 2 honest in-time +
//     leader's σ_L^V = qV at L_0; should still decide)
//   - 2: f+1 slow at n=4 — exits the f-bound on the slow side; OBFT must
//     fall through to a deeper layer where the slow operators' delay no
//     longer matters (deeper-layer broadcast was earlier)
//
// Per-cell outcomes are logged diagnostically; safety enforced by
// RunScenarioOnProtocol's panic gate.
func TestSweep_Asymmetric(t *testing.T) {
	const btt = 200 * time.Millisecond
	slowCounts := []int{0, 1, 2}

	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	for _, slowN := range slowCounts {
		slowN := slowN
		t.Run(fmt.Sprintf("slowN=%d", slowN), func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(4, btt)

			// Mark slowN honest operators (op2..opN) as slow at 2× BTT. op1
			// is the default L_0 leader at K=N convention; keeping it fast
			// preserves leader broadcast timing.
			overrides := make(map[ct.OperatorID]time.Duration, slowN)
			for i := 0; i < slowN; i++ {
				overrides[ct.OperatorID(2+i)] = 2 * btt
			}
			cfg.Network = ct.PerReceiverDelay{
				Inner:     ct.ConstantDelay{D: btt},
				Overrides: overrides,
			}

			// Sanity: at slowN=0 (≡ ConstantDelay baseline), Healthy must
			// decide for both protocols.
			if slowN == 0 {
				for _, p := range protocols {
					out, err := p.Run(cfg)
					require.NoErrorf(t, err, "slowN=%d %s Run", slowN, p.Name())
					require.Truef(t, out.Decided,
						"slowN=0 %s Healthy must decide (matches ConstantDelay baseline)", p.Name())
				}
			}

			var b strings.Builder
			fmt.Fprintf(&b, "\nslowN=%d (slow ops at 2× BTT) catalog matrix:\n", slowN)
			obftTotal, qbftTotal := 0, 0
			obftDecided, qbftDecided := 0, 0
			for _, s := range ct.Catalog {
				obftR := ct.RunScenarioOnProtocol(t, obftadapter.Protocol{}, s, cfg)
				qbftR := ct.RunScenarioOnProtocol(t, qbftadapter.Protocol{}, s, cfg)
				if !obftR.Skipped {
					obftTotal++
					if obftR.Outcome.Decided {
						obftDecided++
					}
				}
				if !qbftR.Skipped {
					qbftTotal++
					if qbftR.Outcome.Decided {
						qbftDecided++
					}
				}
				fmt.Fprintf(&b, "  %-32s OBFT=%-12s QBFT=%-12s\n",
					s.Name, sweepCellSummary(obftR), sweepCellSummary(qbftR))
			}
			fmt.Fprintf(&b, "decision rate: OBFT=%d/%d QBFT=%d/%d\n",
				obftDecided, obftTotal, qbftDecided, qbftTotal)
			t.Log(b.String())
		})
	}
}

// TestSweep_Partition — vary partitioned-operator counts via
// PartitionedNetwork. Verifies BFT-comparison.md Table 3's "Sustained
// partition > absorption window" claim — protocol must miss cleanly with
// no safety violation, regardless of which subset is partitioned. Tier 2
// stress-test addition per CONSENSUS-TEST-PLAN.md (failure-mode validation).
//
// Partition counts:
//   - 0: baseline (no partition; should decide)
//   - 1: f operators isolated at n=4. Cluster has 2f+1 = 3 reachable; at
//     K=N, leader rotation means partitioned operator might be the L_0
//     leader (different scenarios surface differently). Safety must hold;
//     decided varies.
//   - 2: f+1 isolated — exits the f-bound; cluster has < 2f+1 reachable;
//     all protocols must miss cleanly.
func TestSweep_Partition(t *testing.T) {
	const btt = 200 * time.Millisecond
	partitionCounts := []int{0, 1, 2}

	for _, partN := range partitionCounts {
		partN := partN
		t.Run(fmt.Sprintf("partN=%d", partN), func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(4, btt)

			// Partition the last partN operators (op4 first, op3 next, …) to
			// keep op1 (default L_0) reachable.
			partitioned := make(map[ct.OperatorID]bool, partN)
			for i := 0; i < partN; i++ {
				partitioned[ct.OperatorID(4-i)] = true
			}
			cfg.Network = ct.PartitionedNetwork{
				Inner:       ct.ConstantDelay{D: btt},
				Partitioned: partitioned,
			}

			var b strings.Builder
			fmt.Fprintf(&b, "\npartN=%d (isolated ops) catalog matrix:\n", partN)
			obftTotal, qbftTotal := 0, 0
			obftDecided, qbftDecided := 0, 0
			for _, s := range ct.Catalog {
				obftR := ct.RunScenarioOnProtocol(t, obftadapter.Protocol{}, s, cfg)
				qbftR := ct.RunScenarioOnProtocol(t, qbftadapter.Protocol{}, s, cfg)
				if !obftR.Skipped {
					obftTotal++
					if obftR.Outcome.Decided {
						obftDecided++
					}
				}
				if !qbftR.Skipped {
					qbftTotal++
					if qbftR.Outcome.Decided {
						qbftDecided++
					}
				}
				fmt.Fprintf(&b, "  %-32s OBFT=%-12s QBFT=%-12s\n",
					s.Name, sweepCellSummary(obftR), sweepCellSummary(qbftR))
			}
			fmt.Fprintf(&b, "decision rate: OBFT=%d/%d QBFT=%d/%d\n",
				obftDecided, obftTotal, qbftDecided, qbftTotal)
			t.Log(b.String())

			// At partN=2 (> f), cluster cannot reach 2f+1 honest reachable
			// (only 2 operators on the surviving side at n=4). Healthy must
			// miss for both protocols — verifies clean failure, no safety
			// invariant violation (already enforced via panic gate).
			if partN == 2 {
				for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}} {
					out, err := p.Run(cfg)
					require.NoErrorf(t, err, "partN=%d %s Run", partN, p.Name())
					require.Falsef(t, out.Decided,
						"partN=2 (>f isolated) %s Healthy must miss cleanly — < 2f+1 honest reachable", p.Name())
				}
			}
		})
	}
}

// TestSweep_ClockSkew — vary per-operator clock skew via ClockSkewedNetwork
// across the catalog. Verifies the spec's δ-bound claim: consensus tolerates
// per-pair clock differences ≤ δ. Tier 1 stress-test addition per
// CONSENSUS-TEST-PLAN.md (clock skew is a MUST per the protocol's partial-
// synchrony assumption).
//
// The model captures clock skew at the network layer rather than DES timer
// firings: only the *relative* skew between sender and receiver shifts
// effective message timing in the protocol's perceived frame, and the
// relative-skew model is what the spec's δ-bound semantics describe.
//
// Network base delay is `BTT/2 = 100ms`, representing typical-mesh
// propagation (≈ P99). The `BTT` parameter in spec terms decomposes as
// `P99 + δ` — so using base = P99 leaves 2δ = 100ms of slack for pair-wise
// clock skew before the absorption window B_0 = 1 BTT = 200ms is exceeded.
// (The default `ConstantDelay{D: BTT}` saturates B_0; any layered skew on
// top of it is out-of-spec by construction. Clock-skew testing requires
// disaggregating P99 from δ.)
//
// Skew distributions (where δ = 50ms):
//   - zero: baseline (matches default ConstantDelay)
//   - within-bound (pair-wise ≤ δ): per-op skew ∈ {-δ/2, -δ/4, +δ/4, +δ/2};
//     pair-wise max = δ. Within spec — protocol must decide flawlessly.
//   - at-bound (pair-wise = 2δ): per-op skew ∈ {-δ, -δ, +δ, +δ};
//     pair-wise max = 2δ. At the spec edge (B_0 = P99 + 2δ exactly fits).
//     Protocol must decide.
//   - out-of-bound (pair-wise > 2δ): per-op skew ∈ {-2δ, -δ, +δ, +2δ};
//     pair-wise max = 4δ. Above spec — protocol may legitimately miss but
//     safety must hold.
//
// Per-cell outcomes are logged diagnostically; safety enforced by
// RunScenarioOnProtocol's panic gate.
func TestSweep_ClockSkew(t *testing.T) {
	const btt = 200 * time.Millisecond
	const delta = 50 * time.Millisecond // spec δ at Config A
	const baseDelay = btt / 2           // typical-mesh propagation; leaves 2δ for skew

	skewPatterns := []struct {
		name        string
		skew        map[ct.OperatorID]time.Duration
		mustDecide  bool // assert Healthy decides for both protocols
	}{
		{
			name: "zero",
			skew: map[ct.OperatorID]time.Duration{},
			mustDecide: true,
		},
		{
			name: "within-bound-pairwise-delta",
			skew: map[ct.OperatorID]time.Duration{
				1: -delta / 2,
				2: -delta / 4,
				3: +delta / 4,
				4: +delta / 2,
			},
			mustDecide: true,
		},
		{
			name: "at-bound-pairwise-2delta",
			skew: map[ct.OperatorID]time.Duration{
				1: -delta,
				2: -delta,
				3: +delta,
				4: +delta,
			},
			mustDecide: true,
		},
		{
			name: "out-of-bound-pairwise-4delta",
			skew: map[ct.OperatorID]time.Duration{
				1: -2 * delta,
				2: -delta,
				3: +delta,
				4: +2 * delta,
			},
			mustDecide: false, // out-of-spec; safety holds, decision may miss
		},
	}

	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	for _, pat := range skewPatterns {
		pat := pat
		t.Run(pat.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseSweepConfig(4, btt)
			cfg.Network = ct.ClockSkewedNetwork{
				Inner: ct.ConstantDelay{D: baseDelay},
				Skew:  pat.skew,
			}

			// Within spec's δ-bound, Healthy must decide for both protocols.
			// At zero/within/at-bound skew, total pair-wise delay
			// (baseDelay + 2δ skew) ≤ B_0 = 1 BTT, so absorption holds.
			if pat.mustDecide {
				for _, p := range protocols {
					out, err := p.Run(cfg)
					require.NoErrorf(t, err, "%s %s Run", pat.name, p.Name())
					require.Truef(t, out.Decided,
						"%s %s Healthy must decide (within spec's δ-bound)", pat.name, p.Name())
				}
			}

			var b strings.Builder
			fmt.Fprintf(&b, "\nclock-skew %s catalog matrix:\n", pat.name)
			obftTotal, qbftTotal := 0, 0
			obftDecided, qbftDecided := 0, 0
			for _, s := range ct.Catalog {
				obftR := ct.RunScenarioOnProtocol(t, obftadapter.Protocol{}, s, cfg)
				qbftR := ct.RunScenarioOnProtocol(t, qbftadapter.Protocol{}, s, cfg)
				if !obftR.Skipped {
					obftTotal++
					if obftR.Outcome.Decided {
						obftDecided++
					}
				}
				if !qbftR.Skipped {
					qbftTotal++
					if qbftR.Outcome.Decided {
						qbftDecided++
					}
				}
				fmt.Fprintf(&b, "  %-32s OBFT=%-12s QBFT=%-12s\n",
					s.Name, sweepCellSummary(obftR), sweepCellSummary(qbftR))
			}
			fmt.Fprintf(&b, "decision rate: OBFT=%d/%d QBFT=%d/%d\n",
				obftDecided, obftTotal, qbftDecided, qbftTotal)
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
