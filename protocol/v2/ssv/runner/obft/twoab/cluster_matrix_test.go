package twoab

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// 2abOBFT cluster-matrix verification: parameterizes the four
// 2abOBFT runner scenarios — stub Healthy, real-BLS Healthy, real-BLS
// SilentL0Leader_NRFallThrough, real-BLS LateCommit — over the
// (n, K) ∈ {4: 2,3,4} ∪ {7: 3..7} matrix. The non-matrix
// counterparts (TestRunProposerSlot_Healthy_n4 + RealBLS_Healthy_n4 +
// RealBLS_SilentL0Leader_NRFallThrough) each cover one cell; this
// file's matrix variants exercise the protocol across the full
// f+1..N fall-through depth at both cluster sizes:
//
//   - σ-quorum + chained-IBE encryption (Healthy path),
//   - chained-IBE *decryption* via NR-quorum unlock (NR fall-through), and
//   - opportunistic-resolve poll path after TPhase2a (LateCommit),
//
// at every K from BFT-liveness floor (f+1) up to maximum depth (N) —
// which the existing per-scenario tests never exercised at K > 2.
//
// The helpers introduced here (matrixCell, twoabMatrixCells,
// compressedTestOverridesForK) are also consumed by the safety bridge
// in race_safety_bridge_test.go — they live in this non-bridge file
// so a fixture-level regression at K > 2 surfaces here (as a clean
// convergence failure) before the bridge's safety-check overlay
// muddies the diagnostic.

// matrixCell — (n, K) parameterization of a 2abOBFT runner cluster
// test. Mirror of OBFT base's matrixCell (different package — the two
// types are independent).
type matrixCell struct {
	n int
	K int
}

// twoabMatrixCells returns the canonical 2abOBFT cluster matrix:
// n ∈ {4, 7} × K ∈ {f+1..N} = 8 cells. Spans the BFT-liveness floor
// (K=f+1) through the maximum fall-through depth (K=n) for each
// cluster size. Aligned with OBFT base's obftMatrixCells (same set —
// the two protocols share the same n / K relevance range; n=10/13
// are excluded since the safety invariants are size-independent and
// larger cluster sizes inflate wall-time without proportional coverage
// gain).
func twoabMatrixCells() []matrixCell {
	return []matrixCell{
		{4, 2}, {4, 3}, {4, 4},
		{7, 3}, {7, 4}, {7, 5}, {7, 6}, {7, 7},
	}
}

// compressedTestOverridesForK returns ConfigOverrides with compressed
// timing suitable for fast unit tests at any K. Analogous to OBFT
// base's compressedTestScheduleForK — for 2abOBFT the per-layer
// schedule (FetchAt + BroadcastBudget) auto-derives via
// ConfigForCluster from BTT + TPhase2a, so the helper only needs to
// produce (K, BTT, TPhase2a). Same BTT=20ms / TPhase2a=150ms values
// as the existing TestRunProposerSlot_RealBLS_Healthy_n4 fixture.
func compressedTestOverridesForK(K int) *ConfigOverrides {
	return &ConfigOverrides{
		K:                K,
		BTT:              20 * time.Millisecond,
		tPhase2aOverride: 150 * time.Millisecond,
	}
}

// TestRunProposerSlot_RealBLS_Healthy_Matrix runs the Healthy scenario
// at every (n, K) cell using buildBLSCluster + the async blsBus +
// real-BLS crypto, and asserts convergence at each cell. Validates
// that the 2abOBFT runner's K > 2 fall-through machinery (chained
// IBE, layer-rotation, σ-pool gating) is sound end-to-end at the
// non-bridge fixture level — the safety bridge in
// race_safety_bridge_test.go layers race-detector amplification +
// per-Outcome safety checks on top.
//
// The (n=4, K=4) cell overlaps with TestRunProposerSlot_RealBLS_
// Healthy_n4 — that single-cell test is retained as a faster smoke
// check (no matrix loop).
func TestRunProposerSlot_RealBLS_Healthy_Matrix(t *testing.T) {
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runRealBLSHealthyAtCell(t, cell)
		})
	}
}

// runRealBLSHealthyAtCell runs one Healthy slot at (n, K) using
// buildBLSCluster + the compressed-test overrides + the async blsBus.
// Asserts all ops converge on the same Output, decide at L_0, and the
// reconstructed signature verifies against the master pubkey.
//
// Slot id is derived from (n, K) to avoid cross-cell collisions in
// the leader-rotation seed (slot id seeds leaderForLayer's modulus).
func runRealBLSHealthyAtCell(t *testing.T, cell matrixCell) {
	overrides := compressedTestOverridesForK(cell.K)
	cl := buildBLSCluster(t, cell.n, overrides)

	slot := phase0.Slot(100 + cell.n*10 + cell.K)
	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := &blsBus{nodes: cl.nodes, slotStart: slotStart}
	defer bus.stop()
	for _, n := range cl.nodes {
		n := n
		n.broadcastFn = func(data []byte) { bus.broadcast(n.op, data) }
	}

	var wg sync.WaitGroup
	for _, n := range cl.nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.runErr = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()

	var ref *twoabcore.Output
	for _, n := range cl.nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot at n=%d K=%d", n.op, cell.n, cell.K)
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output at n=%d K=%d", n.op, cell.n, cell.K)
		if ref == nil {
			ref = out
			continue
		}
		require.Truef(t, bytes.Equal(ref.Value, out.Value),
			"op %d decided a different Value at n=%d K=%d", n.op, cell.n, cell.K)
		require.Truef(t, bytes.Equal(ref.Signature, out.Signature),
			"op %d reconstructed a different Signature at n=%d K=%d", n.op, cell.n, cell.K)
		require.Equalf(t, ref.Layer, out.Layer,
			"op %d decided a different layer at n=%d K=%d", n.op, cell.n, cell.K)
	}
	require.Equalf(t, 0, ref.Layer, "healthy case decides at L_0 (n=%d K=%d)", cell.n, cell.K)
	require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, ref.Value, ref.Signature),
		"reconstructed signature must verify against master pubkey at n=%d K=%d", cell.n, cell.K)
}

// TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough_Matrix runs
// the silent-L_0-leader NR-fall-through scenario at every (n, K) cell.
// L_0 leader's outbound is suppressed (crashed-sender sim); peers must
// detect via NoValue, aggregate qEnc NR partials at L_0, unlock the
// chain key to decrypt the L_1 SigmaChained entries, and decide at
// L_1. This exercises real chained-IBE *decryption* — the symmetric
// counterpart to the Healthy matrix (which only exercises IBE encryption).
//
// At K=2 (n=4) this is the canonical f+1=K case — only one fall-
// through layer. At K>2 the cluster could in principle fall further
// (e.g., if L_1 were also silent), but the scenario keeps only L_0
// silent so every cell deterministically decides at L_1.
//
// The (n=4, K=4) cell overlaps with TestRunProposerSlot_RealBLS_
// SilentL0Leader_NRFallThrough — that single-cell test is retained
// as a faster smoke check.
func TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough_Matrix(t *testing.T) {
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runRealBLSSilentL0LeaderAtCell(t, cell)
		})
	}
}

// runRealBLSSilentL0LeaderAtCell runs one silent-L_0-leader slot at
// (n, K) using buildBLSCluster + the silent-set blsBus. Asserts all
// ops converge, decide at L_1, and the reconstructed L_1 signature
// verifies against the master pubkey.
func runRealBLSSilentL0LeaderAtCell(t *testing.T, cell matrixCell) {
	overrides := compressedTestOverridesForK(cell.K)
	cl := buildBLSCluster(t, cell.n, overrides)

	slot := phase0.Slot(200 + cell.n*10 + cell.K)
	committee := make([]spectypes.OperatorID, cell.n)
	for i := 0; i < cell.n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}
	lZeroLeader := spectypes.OperatorID(leaderForLayer(committee, twoabcore.Height(slot), 0))

	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := &blsBus{
		nodes:     cl.nodes,
		slotStart: slotStart,
		silent:    map[spectypes.OperatorID]bool{lZeroLeader: true},
	}
	defer bus.stop()
	for _, n := range cl.nodes {
		n := n
		n.broadcastFn = func(data []byte) { bus.broadcast(n.op, data) }
	}

	var wg sync.WaitGroup
	for _, n := range cl.nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.runErr = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()

	var ref *twoabcore.Output
	for _, n := range cl.nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot at n=%d K=%d", n.op, cell.n, cell.K)
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output (NR fall-through failed) at n=%d K=%d", n.op, cell.n, cell.K)
		if ref == nil {
			ref = out
			continue
		}
		require.Truef(t, bytes.Equal(ref.Value, out.Value),
			"op %d decided a different Value at n=%d K=%d", n.op, cell.n, cell.K)
		require.Truef(t, bytes.Equal(ref.Signature, out.Signature),
			"op %d reconstructed a different Signature at n=%d K=%d", n.op, cell.n, cell.K)
		require.Equalf(t, ref.Layer, out.Layer,
			"op %d decided a different layer at n=%d K=%d", n.op, cell.n, cell.K)
	}
	require.Equalf(t, 1, ref.Layer,
		"silent L_0 leader → cluster decides at L_1 (n=%d K=%d)", cell.n, cell.K)
	require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, ref.Value, ref.Signature),
		"reconstructed L_1 signature must verify against master pubkey at n=%d K=%d", cell.n, cell.K)
}

// lateValueDelayPredicate returns a delayFn that delays KindValue
// arrivals at the victim op from the highest-numbered (n - qV + 1)
// peers, leaving the victim's timely σ-pool short of qV until the
// delayed values arrive. Exercises the 2abOBFT runner's opportunistic-
// resolve poll path: after Phase-2a fires, the scheduler re-runs
// Resolve on every state-delta until the relay-submission cutoff (or
// success), so late KindValues that bring σ-pool ≥ qV at the victim
// still produce a successful submit.
//
// Protocol note — why KindValue, not KindCommit: in 2abOBFT, KindValue
// is the σ-side terminal emission and carries the emitter's L_0 σ
// partial (`ValueMsg.L0Partial`); KindCommit carries NR partials (NR-
// side) or L_k>0 SigmaChained entries. Delaying KindCommit would NOT
// affect L_0 σ-pool fill, so the LateCommit name is borrowed from
// OBFT base's parallel scenario but the wire-kind being delayed
// differs by protocol. See protocol/v2/obft/twoab/messages.go for the
// ValueMsg.L0Partial spec.
//
// At n=4 / qV=3: delays ops 3, 4 (2 peers); leaves op2 timely + own
// self-observation = 2 partials, < qV=3 — opportunistic resolve must
// salvage the victim when the delayed values arrive.
// At n=7 / qV=5: delays ops 5, 6, 7 (3 peers); leaves ops 2-4 timely +
// own = 4 partials, < qV=5 — same regression shape, deeper fall depth.
//
// Mirror of OBFT base's lateCommitDelayPredicate; different wire-kind
// delayed but the (n, qV) arithmetic + intent (delay σ-pool fill at
// victim) are identical.
func lateValueDelayPredicate(n int, victim spectypes.OperatorID, delay time.Duration) delayFn {
	f := (n - 1) / 3
	qV := 2*f + 1
	delayCount := n - qV + 1
	firstDelayedOp := spectypes.OperatorID(n - delayCount + 1)
	return func(from, to spectypes.OperatorID, kind byte) time.Duration {
		if kind == byte(wire.KindValue) && to == victim && from >= firstDelayedOp && int(from) <= n {
			return delay
		}
		return 0
	}
}

// TestRunProposerSlot_RealBLS_LateCommit_Matrix runs the late-σ-pool-
// fill scenario at every (n, K) cell. A parameterized subset of
// KindValue arrivals at op1 (the victim) is delayed past Phase-2a's
// fire time, so the victim's σ-pool stays short of qV when its initial
// Resolve fires. The 2abOBFT scheduler must opportunistically re-fire
// Resolve on the state-delta channel when the late values land and
// complete σ-quorum at L_0.
//
// Counterpart to OBFT base's TestSafetyBridge_OBFT_LateCommit and
// TestRunProposerSlot_LateCommit_OpportunisticResolve. Both protocols
// share the opportunistic-resolve poll semantics, but the wire-kind
// carrying σ partials differs — see lateValueDelayPredicate's
// protocol note for the KindValue-vs-KindCommit asymmetry.
//
// The "LateCommit" in the test name is kept for parity with OBFT
// base's scenario of the same name (which delays KindCommit, its
// own σ-side terminal); the helper is named lateValueDelayPredicate
// to reflect the 2abOBFT-accurate wire kind it actually delays.
func TestRunProposerSlot_RealBLS_LateCommit_Matrix(t *testing.T) {
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runRealBLSLateCommitAtCell(t, cell)
		})
	}
}

// runRealBLSLateCommitAtCell runs one late-σ-pool-fill slot at (n, K)
// using buildBLSCluster + a delay-aware blsBus. Asserts all ops
// (including the victim) submit, decide at L_0 via opportunistic
// resolve, and the reconstructed signature verifies against the master
// pubkey.
//
// Delay choice: 300ms > TPhase2a=150ms by 2·BTT_test, but well within
// the default resolveBudget (TPhase2a + BTT + max(SafetyBuffer, BTT) +
// ε_3 + jitter + headerHeadroom ≈ 1070ms with the compressed config).
// So the late values arrive after the initial Resolve fired but well
// before the relay-submission cutoff — the opportunistic poll has
// time to pick them up and finish σ-quorum.
func runRealBLSLateCommitAtCell(t *testing.T, cell matrixCell) {
	overrides := compressedTestOverridesForK(cell.K)
	cl := buildBLSCluster(t, cell.n, overrides)

	slot := phase0.Slot(400 + cell.n*10 + cell.K)
	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const lateValueDelay = 300 * time.Millisecond
	const victim spectypes.OperatorID = 1
	bus := newBlsBusWithDelay(cl.nodes, slotStart, lateValueDelayPredicate(cell.n, victim, lateValueDelay))
	defer bus.stop()
	for _, n := range cl.nodes {
		n := n
		n.broadcastFn = func(data []byte) { bus.broadcast(n.op, data) }
	}

	var wg sync.WaitGroup
	for _, n := range cl.nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.runErr = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()

	var ref *twoabcore.Output
	for _, n := range cl.nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot at n=%d K=%d", n.op, cell.n, cell.K)
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output (opportunistic resolve failed for late-commit case) at n=%d K=%d", n.op, cell.n, cell.K)
		if ref == nil {
			ref = out
			continue
		}
		require.Truef(t, bytes.Equal(ref.Value, out.Value),
			"op %d decided a different Value at n=%d K=%d", n.op, cell.n, cell.K)
		require.Truef(t, bytes.Equal(ref.Signature, out.Signature),
			"op %d reconstructed a different Signature at n=%d K=%d", n.op, cell.n, cell.K)
		require.Equalf(t, ref.Layer, out.Layer,
			"op %d decided a different layer at n=%d K=%d", n.op, cell.n, cell.K)
	}
	require.Equalf(t, 0, ref.Layer,
		"late-commit case decides at L_0 via opportunistic resolve (n=%d K=%d)", cell.n, cell.K)
	require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, ref.Value, ref.Signature),
		"reconstructed signature must verify against master pubkey at n=%d K=%d", cell.n, cell.K)
}

// TestRunProposerSlot_Healthy_Matrix runs the stub-crypto Healthy
// scenario at every (n, K) cell using buildSmokeCluster + the
// synchronous smokeBus. Cheap smoke check (stub Signer + StubIBE);
// the real-BLS matrix variant covers the same cells under real crypto.
//
// The (n=4, K=2) cell overlaps with TestRunProposerSlot_Healthy_n4 —
// that single-cell test is retained as a faster smoke check at K=2.
func TestRunProposerSlot_Healthy_Matrix(t *testing.T) {
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runStubHealthyAtCell(t, cell)
		})
	}
}

// runStubHealthyAtCell runs one Healthy slot at (n, K) using
// buildSmokeCluster + the synchronous smokeBus + stub crypto. Asserts
// all ops converge on the L_0 leader's candidate value.
func runStubHealthyAtCell(t *testing.T, cell matrixCell) {
	overrides := compressedTestOverridesForK(cell.K)
	nodes := buildSmokeCluster(t, cell.n, overrides)

	slot := phase0.Slot(300 + cell.n*10 + cell.K)
	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	bus := &smokeBus{
		ctx:       ctx,
		slotStart: slotStart,
		nodes:     make(map[spectypes.OperatorID]*smokeNode, len(nodes)),
	}
	for _, n := range nodes {
		n := n
		n.broadcastFn = func(data []byte) { bus.broadcast(n.op, data) }
		bus.nodes[n.op] = n
	}

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()

	var ref *twoabcore.Output
	for _, n := range nodes {
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output at n=%d K=%d", n.op, cell.n, cell.K)
		if ref == nil {
			ref = out
			continue
		}
		require.Equalf(t, ref.Value, out.Value,
			"op %d decided a different Value at n=%d K=%d", n.op, cell.n, cell.K)
		require.Equalf(t, ref.Signature, out.Signature,
			"op %d reconstructed a different Signature at n=%d K=%d", n.op, cell.n, cell.K)
	}
	require.Equalf(t, []byte("L0-V"), []byte(ref.Value),
		"cluster decides the L_0 leader's candidate at n=%d K=%d", cell.n, cell.K)
}
