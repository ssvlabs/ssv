package stresstest

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Default sim count per cell. Override with the OBFT_STRESS_COUNT env var
// for scale runs without touching the source. Most cells in this suite are
// algebra-deterministic (the byz pattern fully determines outcome at given
// network settings) and don't need many samples; the jittered-network
// cells are where higher counts justify themselves.
const defaultSimsPerCell = 100

// simsPerCell returns the configured per-cell sim count. Honors
// OBFT_STRESS_COUNT for scale runs.
func simsPerCell() int {
	if s := os.Getenv("OBFT_STRESS_COUNT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return defaultSimsPerCell
}

// bftStartAnchored returns a SimConfig anchored at BFT_start = T_broadcast_max,
// matching the BFT-comparison.md framing where "BFT_start" is the moment Phase
// 1 broadcast begins (i.e., the leader has already pre-fetched). This is the
// correct anchoring for budget-fit comparisons against the doc's tables.
//
// T_commit = BFT_start + 2(D+δ) (Phase-1 propagation slack)
// T_round_end = BFT_start + 5(D+δ) + ε_3 (5(D+δ) is OBFT's actual wall-clock)
func bftStartAnchored(bftStart, d time.Duration) SimConfig {
	delta := 50 * time.Millisecond
	delta2 := 2 * (d + delta)
	delta3 := (d + delta) + 100*time.Millisecond
	tCommit := bftStart + 2*(d+delta)
	// Per-layer FetchAt must be in [0, T_broadcast_max=bftStart]. Spread
	// across that window monotonically decreasing in k.
	K := 4
	fetchAt := make([]time.Duration, K)
	if bftStart <= 0 {
		// Edge case: BFT_start = 0 means broadcast at virtual-time 0; no
		// pre-fetch budget. All layers share fetchAt = 0.
		for k := 0; k < K; k++ {
			fetchAt[k] = 0
		}
	} else {
		// Symmetric K-tier spread: T_0 = bftStart, T_{K-1} = bftStart/K.
		step := bftStart / time.Duration(K)
		for k := 0; k < K; k++ {
			fetchAt[k] = bftStart - time.Duration(k)*step
		}
	}
	return SimConfig{
		N:       4,
		K:       K,
		TCommit: tCommit,
		Delta2:  delta2,
		Delta3:  delta3,
		D:       d,
		Delta:   delta,
		FetchAt: fetchAt,
		Network: ConstantDelay{D: d},
		Byz:     ByzNone{},
		Host:    HostAllValid{},
	}
}

// baselineConfig returns a SimConfig template at Config A (D=100ms,
// δ=50ms, Δ_2=2(D+δ), Δ_3=(D+δ)+ε_3) scaled by D.
//
// Note: anchors T_commit at 1500ms which is OBFT.md's Config A choice for
// D=100ms. For higher D the auto-push preserves T_broadcast_max ≥ 0 but
// compresses fetch budget below realistic levels — for budget-fit
// comparisons against BFT-comparison.md's BFT_start axis, prefer
// bftStartAnchored().
func baselineConfig(d time.Duration) SimConfig {
	delta := 50 * time.Millisecond
	delta2 := 2 * (d + delta)
	delta3 := (d + delta) + 100*time.Millisecond
	tCommit := 1500 * time.Millisecond
	// At higher D, T_broadcast_max may go negative — push T_commit out.
	if tCommit < 2*(d+delta)+100*time.Millisecond {
		tCommit = 2*(d+delta) + 100*time.Millisecond
	}
	return SimConfig{
		N:       4,
		K:       4,
		TCommit: tCommit,
		Delta2:  delta2,
		Delta3:  delta3,
		D:       d,
		Delta:   delta,
		Network: ConstantDelay{D: d},
		Byz:     ByzNone{},
		Host:    HostAllValid{},
	}
}

// runResult bundles an Outcome with the seed used to produce it, so failed
// assertions can be replayed deterministically.
type runResult struct {
	Outcome Outcome
	Seed    int64
}

// runMany runs `n` simulations of `cfg` (each with a unique seed derived
// from `baseSeed`) and returns the per-sim results.
func runMany(t *testing.T, cfg SimConfig, baseSeed int64, n int) []runResult {
	t.Helper()
	out := make([]runResult, n)
	for i := 0; i < n; i++ {
		c := cfg
		c.Seed = baseSeed*int64(n+1) + int64(i)
		o, err := Run(c)
		if err != nil {
			t.Fatalf("sim %d (seed=%d): Run error: %v", i, c.Seed, err)
		}
		out[i] = runResult{Outcome: o, Seed: c.Seed}
	}
	return out
}

// replayHint returns a string the user can paste into a debugger / one-off
// test to reproduce the exact failing sim with trace enabled.
func replayHint(cfg SimConfig, seed int64) string {
	return fmt.Sprintf("replay: cfg.Seed = %d; cfg.TraceEnabled = true; Run(cfg)", seed)
}

// assertAll asserts every outcome in `outs` matches `expect`. On mismatch,
// dumps the first failing outcome's seed + a truncated description so the
// user can replay it. The seed → trace mapping is deterministic per
// determinism guarantee (TestStress_TraceDeterministic).
func assertAll(t *testing.T, cfg SimConfig, outs []runResult, expect ExpectClass) {
	t.Helper()
	failed := 0
	var firstFailure string
	for i, r := range outs {
		ok, why := CheckExpectation(r.Outcome, expect)
		if !ok {
			failed++
			if firstFailure == "" {
				firstFailure = fmt.Sprintf("sim %d (seed=%d): %s\n  outcome: %s\n  %s",
					i, r.Seed, why, r.Outcome, replayHint(cfg, r.Seed))
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d/%d sims failed expectation %s\n%s",
			failed, len(outs), expect, firstFailure)
	}
}

// ---- Table 1: success-mode (healthy path) ------------------------------

// TestStress_T1_Healthy sweeps D and verifies that under all-honest, no-byz,
// no-network-failure conditions, OBFT decides at L_0 with all operators
// agreeing on the canonical V.
//
// Note on doc-vs-actual timing: docs/BFT-comparison.md uses "3D" as an
// RTT-count approximation, treating phases as exactly D each. Actual OBFT
// timing under the spec parameters is T_round_end = T_commit + 3(D+δ) + ε_3,
// noticeably more than 3D once T_commit is included. Budget-fit cells in
// the doc are computed against 3D; the sim measures real T_round_end. We
// log the discrepancy via the BudgetReport test rather than asserting
// against the doc's loose numbers.
func TestStress_T1_Healthy(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
	}{
		{"D=200ms", 200 * time.Millisecond},
		{"D=600ms", 600 * time.Millisecond},
		{"D=1000ms", 1000 * time.Millisecond},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := baselineConfig(c.d)
			outs := runMany(t, cfg, 1, simsPerCell())
			assertAll(t, cfg, outs, ExpectSuccessHealthy)
		})
	}
}

// TestStress_BudgetReport surfaces the actual T_round_end the protocol
// achieves at each (BFT_start, D) cell from BFT-comparison.md Table 1, so
// we can compare against the doc's 3D/4D RTT-count predictions. This test
// LOGS rather than asserts; it's diagnostic.
//
// Uses bftStartAnchored (BFT_start = T_broadcast_max), matching the doc's
// framing of "BFT_start = moment Phase 1 broadcast begins". With this
// anchoring, OBFT's actual completion = BFT_start + 5(D+δ) + ε_3.
func TestStress_BudgetReport(t *testing.T) {
	starts := []time.Duration{0, 1 * time.Second, 2500 * time.Millisecond}
	ds := []time.Duration{200 * time.Millisecond, 600 * time.Millisecond, 1000 * time.Millisecond}

	t.Logf("Cell                  | T_round_end | (post-BFT_start) | Budget | Fits | Doc 3D | Doc fits")
	t.Logf("----------------------+-------------+------------------+--------+------+--------+----------")
	for _, start := range starts {
		for _, d := range ds {
			cfg := bftStartAnchored(start, d)
			tRoundEnd := cfg.TCommit + cfg.Delta2 + cfg.Delta3
			postStart := tRoundEnd - start
			budget := 4*time.Second - start - 250*time.Millisecond
			fits := postStart <= budget
			docPredicts3D := 3 * d
			docFits := docPredicts3D <= budget
			marker := ""
			if fits != docFits {
				marker = "  ← DISAGREES WITH DOC"
			}
			t.Logf("start=%-5v,D=%-6v | %-11v | %-16v | %-6v | %-4v | %-6v | %v%s",
				start, d, tRoundEnd, postStart, budget, fits, docPredicts3D, docFits, marker)
		}
	}
}

// ---- Table 3: silent-leader fall-through ------------------------------

// TestStress_T3_SilentLeaderL0 verifies that when L_0 leader is silent,
// every honest operator falls through to a deeper layer in-round.
func TestStress_T3_SilentLeaderL0(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzSilentLeader{Byz: 1, Layer: 0} // op1 leads L_0 by rotation (slot=1, k=0 → idx=1 mod 4)
	outs := runMany(t, cfg, 2, simsPerCell())
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
}

// ---- Table 3: multi-leader silent (K-1=3 silent) ----------------------

// TestStress_T3_MultiSilent verifies in-round K-layer fall-through when
// the first 3 layers are silent.
func TestStress_T3_MultiSilent(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzMultiSilent{OnlyHonestLayer: 3}
	outs := runMany(t, cfg, 3, simsPerCell())
	for i, r := range outs {
		o := r.Outcome
		if !o.Decided {
			t.Fatalf("sim %d (seed=%d): expected fall-through to L_3; got MISS\n%s", i, r.Seed, o)
		}
		if o.Layer != 3 {
			t.Fatalf("sim %d (seed=%d): expected L_3; got L_%d", i, r.Seed, o.Layer)
		}
		if !o.AllAgree() {
			t.Fatalf("sim %d (seed=%d): operators disagreed", i, r.Seed)
		}
	}
}

// ---- Table 3: equivocation σ-locked split (1-1-Defer) -----------------

// TestStress_T3_EquivocSigmaLockedSplit verifies that a byz L_0 leader
// who delivers V to one honest, V' to another, nothing to the rest,
// reliably slot-misses (σ-locked operators block NR-quorum at L_0).
func TestStress_T3_EquivocSigmaLockedSplit(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzEquivocSigmaLockedSplit{Byz: 1, RecipientA: 2, RecipientB: 3}
	outs := runMany(t, cfg, 4, simsPerCell())
	assertAll(t, cfg, outs, ExpectMiss)
	// Equivocation evidence (Rule 2) should accumulate at every receiver
	// of two distinct bundles — but at this delivery shape, no honest
	// receives both, so we can't assert Rule 2 at every operator. We can
	// assert at most: total evidence is bounded.
	for i, r := range outs {
		if r.Outcome.Evidence[obft.EvidenceLeaderEquivocation] > 0 {
			// Some configurations produce evidence (e.g., op1's self-
			// observation captures both bundles). That's fine.
			_ = i
		}
	}
}

// ---- Table 3: equivocation 1-1-1 split --------------------------------

// TestStress_T3_Equivoc111 verifies the 1-1-1 split slot-misses.
func TestStress_T3_Equivoc111(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzEquivoc111{Byz: 1}
	outs := runMany(t, cfg, 5, simsPerCell())
	assertAll(t, cfg, outs, ExpectMiss)
}

// ---- Table 3: equivocation all-Defer fall-through ---------------------

// TestStress_T3_EquivocAllDefer verifies that when byz delivers both V's
// to all 3 honest, they all retain ≥ 2 distinct V's, force-NR at end of
// Phase 2, and fall through to L_1.
func TestStress_T3_EquivocAllDefer(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzEquivocAllDefer{Byz: 1}
	outs := runMany(t, cfg, 6, simsPerCell())
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
	// Every honest non-leader should have observed Rule 2 evidence at L_0.
	for i, r := range outs {
		if r.Outcome.Evidence[obft.EvidenceLeaderEquivocation] == 0 {
			t.Fatalf("sim %d (seed=%d): expected Rule 2 evidence; got none\n%s", i, r.Seed, r.Outcome)
		}
	}
}

// ---- Table 3: validity-divergence 2-2 split ---------------------------

// TestStress_T3_ValidityDivergence_2_2 verifies the 2-2 algebraic limit:
// host returns NV for 2 of 4 operators on V_{L_0}. At L_0, σ-pool = 2
// (leader's σ_V + 1 valid honest) < qV=3; NR-pool = 2 (NV honest) <
// qEnc=3. Neither quorum reaches; fall-through to L_1 is blocked because
// chained-decryption at L_1 requires NR-quorum at L_0. Slot misses cleanly
// per BFT-comparison.md Table 3 "2-2 split → ✗ algebraic limit".
func TestStress_T3_ValidityDivergence_2_2(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Host = HostInvalidForOperators{
		Layer:     0,
		Operators: map[obft.OperatorID]bool{3: true, 4: true},
	}
	outs := runMany(t, cfg, 7, simsPerCell())
	assertAll(t, cfg, outs, ExpectMiss)
}

// realBLSSimsPerCell is the per-cell sample size for real-BLS-backed
// tests. Real BLS is ~50–100× slower than stub; equivocation outcomes
// are deterministic given the byz pattern, so a small sample size is
// sufficient to verify the cryptographic invariant alongside the
// protocol-layer expectation.
const realBLSSimsPerCell = 30

// realBLSSetup returns a baseline cfg with BLSKeys populated; reusable
// across the real-BLS test suite.
func realBLSSetup(t *testing.T, d time.Duration) (SimConfig, *BLSKeys) {
	t.Helper()
	keys, err := GenerateBLSKeys([]obft.OperatorID{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("generate BLS keys: %v", err)
	}
	cfg := baselineConfig(d)
	cfg.BLSKeys = keys
	return cfg, keys
}

// ---- Table 3: equivocation tests with real BLS -------------------------

// TestStress_T3_EquivocSigmaLockedSplit_RealBLS reproduces the σ-locked
// split test with real BLSSigner partials. Verifies that real partial-sig
// verification distinguishes valid σ partials per V_a vs V_b correctly,
// and that the quorum algebra (σ-pools partitioned by V, NR-pool blocked
// by σ-locked operators) holds end-to-end with real cryptography.
func TestStress_T3_EquivocSigmaLockedSplit_RealBLS(t *testing.T) {
	cfg, _ := realBLSSetup(t, 200*time.Millisecond)
	cfg.Byz = ByzEquivocSigmaLockedSplit{Byz: 1, RecipientA: 2, RecipientB: 3}
	outs := runMany(t, cfg, 81, realBLSSimsPerCell)
	assertAll(t, cfg, outs, ExpectMiss)
}

// TestStress_T3_Equivoc111_RealBLS reproduces 1-1-1 split with real BLS.
// Three distinct V's, three honest operators — each σ-locks on a different
// V before observing equivocation; σ-pools split below qV cluster-wide;
// no NR-quorum either; slot misses.
func TestStress_T3_Equivoc111_RealBLS(t *testing.T) {
	cfg, _ := realBLSSetup(t, 200*time.Millisecond)
	cfg.Byz = ByzEquivoc111{Byz: 1}
	outs := runMany(t, cfg, 82, realBLSSimsPerCell)
	assertAll(t, cfg, outs, ExpectMiss)
}

// TestStress_T3_EquivocAllDefer_RealBLS reproduces all-Defer fall-through
// with real BLS. Byz floods both V's to all honest; every honest retains
// ≥ 2 distinct V's and force-NRs at end of Phase 2; NR-quorum at L_0 →
// in-round fall-through to L_1 with honest L_1 leader. Real BLS verifies
// that NR-partial aggregation produces a chained-decryption key that
// successfully unlocks L_1 entries — closing the loop on the cryptographic
// invariant the protocol relies on.
func TestStress_T3_EquivocAllDefer_RealBLS(t *testing.T) {
	cfg, _ := realBLSSetup(t, 200*time.Millisecond)
	cfg.Byz = ByzEquivocAllDefer{Byz: 1}
	outs := runMany(t, cfg, 83, realBLSSimsPerCell)
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
	for i, r := range outs {
		if r.Outcome.Evidence[obft.EvidenceLeaderEquivocation] == 0 {
			t.Fatalf("sim %d (seed=%d): expected Rule 2 evidence; got none\n%s",
				i, r.Seed, r.Outcome)
		}
	}
}

// TestStress_T3_HV1SelectiveDelivery_RealBLS reproduces h_V=1 with real
// BLS. Verifies that the (σ-pool, NR-pool) algebra at L_0 matches the
// deadlock prediction with cryptographically-verified partial sigs.
func TestStress_T3_HV1SelectiveDelivery_RealBLS(t *testing.T) {
	cfg, _ := realBLSSetup(t, 200*time.Millisecond)
	cfg.Byz = ByzHV1SelectiveDelivery{Byz: 1, Recipient: 2}
	outs := runMany(t, cfg, 84, realBLSSimsPerCell)
	assertAll(t, cfg, outs, ExpectMiss)
}

// ---- Table 3: Rule 4 (fake encrypted presence at k > 0) --------------

// TestStress_T3_FakeEncryptedPresence verifies that a byz that suppresses
// their Phase-1 broadcast at L_0 (so NR-quorum at L_0 reaches → unlocks
// L_1 chained decryption) and substitutes garbage bytes for their L_1
// Onion entry produces:
//   - Slot succeeds at L_1 (honest L_1 leader's bundle propagates;
//     three honest operators contribute valid σ partials at L_1).
//   - Rule 4 (EvidenceFakeEncryptedPresence) recorded against byz at
//     every honest receiver that attempted to decrypt byz's L_1 entry.
func TestStress_T3_FakeEncryptedPresence(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzFakeEncryptedPresence{Byz: 1, SilentLayer: 0, GarbageLayer: 1}
	outs := runMany(t, cfg, 8, simsPerCell())
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
	for i, r := range outs {
		if r.Outcome.Layer != 1 {
			t.Fatalf("sim %d (seed=%d): expected decision at L_1; got L_%d",
				i, r.Seed, r.Outcome.Layer)
		}
		if r.Outcome.Evidence[obft.EvidenceFakeEncryptedPresence] == 0 {
			t.Fatalf("sim %d (seed=%d): expected Rule 4 evidence; got none\n%s",
				i, r.Seed, r.Outcome)
		}
	}
}

// TestStress_T3_FakeEncryptedPresence_RealBLS verifies the same Rule 4
// scenario with real BLSSigner + TLockIBE, exercising the cryptographic
// IBE's AES-GCM auth-failure path (rather than the stub's format-check
// path). Stub IBE catches Rule 4 via "malformed ciphertext" early; real
// TLockIBE catches it via "AES-GCM body fails to authenticate" after
// the IBE outer wrap is removed. Both flows record EvidenceFakeEncryptedPresence
// against the byz; this test exercises the real-BLS path explicitly so
// the cryptographic invariant is verified end-to-end.
func TestStress_T3_FakeEncryptedPresence_RealBLS(t *testing.T) {
	keys, err := GenerateBLSKeys([]obft.OperatorID{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("generate BLS keys: %v", err)
	}
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzFakeEncryptedPresence{Byz: 1, SilentLayer: 0, GarbageLayer: 1}
	cfg.BLSKeys = keys

	// Real BLS is ~50–100× slower than stub. Use a smaller per-cell count
	// for this test; the protocol behavior is identical to the stub
	// version, so sample size only matters for Rule 4 detection — which
	// is deterministic given the byz pattern.
	const realBLSSims = 50
	outs := runMany(t, cfg, 80, realBLSSims)
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
	for i, r := range outs {
		if r.Outcome.Layer != 1 {
			t.Fatalf("sim %d (seed=%d): expected decision at L_1; got L_%d",
				i, r.Seed, r.Outcome.Layer)
		}
		if r.Outcome.Evidence[obft.EvidenceFakeEncryptedPresence] == 0 {
			t.Fatalf("sim %d (seed=%d): expected Rule 4 evidence; got none\n%s",
				i, r.Seed, r.Outcome)
		}
	}
}

// ---- Table 3: h_V=1 selective-delivery deadlock ------------------------

// TestStress_T3_HV1SelectiveDelivery verifies the spec §Failure modes
// h_V=1 deadlock: byz L_0 leader delivers their Phase-1 bundle to
// exactly one honest operator and emits a normal Phase-2 Onion. Other
// honest see byz's σ at L_0 via the L_0 no-V fallback rule (auth-signed
// Onion claiming σ at L_0 → Defer-due-to-partition). At end of Phase 2
// they force-NR.
//
//   - σ-pool at L_0 cluster-wide: byz's σ_L^V (visible only to Recipient)
//     + Recipient's σ = 2 < qV=3.
//   - NR-pool at L_0: (n - 2) honest force-NR; byz σ-locked, doesn't NR.
//     = 2 < qEnc=3.
//   - Neither quorum reaches; fall-through blocked. Slot misses.
//
// This is BFT-comparison.md Table 3's "h_V=1 selective-delivery deadlock:
// ✗ slot miss" for OBFT.
func TestStress_T3_HV1SelectiveDelivery(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzHV1SelectiveDelivery{Byz: 1, Recipient: 2}
	outs := runMany(t, cfg, 9, simsPerCell())
	assertAll(t, cfg, outs, ExpectMiss)
}

// ---- Jittered network sweeps ------------------------------------------

// TestStress_JitterHealthy stress-tests the healthy path with realistic
// network jitter. Per-message delays are uniform on [D-jitter, D+jitter];
// each sim with a different seed produces a different message ordering.
// The expected outcome is unchanged (healthy path → L_0 success), but
// running thousands of seeds verifies the protocol doesn't have any
// timing-sensitive bugs that flip outcome under reordering.
func TestStress_JitterHealthy(t *testing.T) {
	jitters := []time.Duration{
		0,                       // sanity: zero jitter == ConstantDelay
		20 * time.Millisecond,   // 10% of D=200ms
		60 * time.Millisecond,   // 30% — significant reordering
	}
	for _, j := range jitters {
		j := j
		t.Run(fmt.Sprintf("jitter=%v", j), func(t *testing.T) {
			t.Parallel()
			cfg := baselineConfig(200 * time.Millisecond)
			cfg.Network = JitteredDelay{D: 200 * time.Millisecond, Jitter: j}
			outs := runMany(t, cfg, 100+int64(j), simsPerCell())
			assertAll(t, cfg, outs, ExpectSuccessHealthy)
		})
	}
}

// TestStress_JitterEquivocAllDefer stress-tests the all-Defer fall-through
// pattern with jitter — the byz delivers both V's to all honest, but order
// of arrival depends on per-message delay. Verifies that regardless of
// which V each honest sees first, the cluster falls through to L_1.
func TestStress_JitterEquivocAllDefer(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Byz = ByzEquivocAllDefer{Byz: 1}
	cfg.Network = JitteredDelay{D: 200 * time.Millisecond, Jitter: 50 * time.Millisecond}
	outs := runMany(t, cfg, 200, simsPerCell())
	assertAll(t, cfg, outs, ExpectSuccessFallThrough)
	for i, r := range outs {
		if r.Outcome.Evidence[obft.EvidenceLeaderEquivocation] == 0 {
			t.Fatalf("sim %d (seed=%d): expected equivocation evidence; got none\n%s", i, r.Seed, r.Outcome)
		}
	}
}

// ---- Trace replay sanity ----------------------------------------------

// TestStress_TraceDeterministic runs the same config twice with the same
// seed and verifies the trace is byte-identical. Regression test for the
// determinism guarantee.
func TestStress_TraceDeterministic(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.Seed = 42
	cfg.TraceEnabled = true

	o1, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	o2, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(o1.Trace) != len(o2.Trace) {
		t.Fatalf("trace lengths differ: %d vs %d", len(o1.Trace), len(o2.Trace))
	}
	for i := range o1.Trace {
		if o1.Trace[i] != o2.Trace[i] {
			t.Fatalf("trace[%d] differs:\n  run1: %v %s\n  run2: %v %s",
				i, o1.Trace[i].When, o1.Trace[i].Event,
				o2.Trace[i].When, o2.Trace[i].Event)
		}
	}
}

// ---- Smoke ------------------------------------------------------------

// TestStress_Smoke runs one sim end-to-end with trace and prints a
// fragment to surface event ordering for visual inspection. Useful when
// onboarding the harness; not load-bearing.
func TestStress_Smoke(t *testing.T) {
	cfg := baselineConfig(200 * time.Millisecond)
	cfg.TraceEnabled = true
	cfg.Seed = 1
	o, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Decided {
		t.Fatalf("smoke: expected decision; got %s\nTrace:\n%s", o, o.FormatTrace())
	}
	// Print first ~10 events for human inspection (only on -v).
	if testing.Verbose() {
		lines := strings.Split(o.FormatTrace(), "\n")
		for i, l := range lines {
			if i >= 12 {
				break
			}
			t.Log(l)
		}
		t.Logf("outcome: %s", o)
	}
}
