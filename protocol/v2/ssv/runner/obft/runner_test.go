package obft

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// End-to-end runner test: drives n=4 operators through one healthy proposer-
// duty slot using RunProposerSlot, with sub-second timing so tests run fast.
// Verifies all operators converge on the same Output and that SubmitOutput
// fires.

// compressedTestSchedule returns the (BroadcastBudget, FetchAt) pair for the
// compressed-timing test fixtures (TCommit=200ms / BTT=30ms) using the
// spec-default budget schedule. Deepest B_3 = T_commit ("earliest possible"
// per spec §Setting); FetchAt for the deepest layer is therefore 0.
func compressedTestSchedule(t *testing.T) (broadcastBudget, fetchAt []time.Duration) {
	t.Helper()
	var err error
	// RefloodDelay=0 in the compressed-timing test fixture: the small
	// TCommit=200ms / BTT=30ms scale can't accommodate production's 700ms
	// RefloodDelay. Tests exercising RefloodDelay-aware sizing use full-
	// scale BTT and tCommit values.
	broadcastBudget, err = DefaultBroadcastBudgetSchedule(4, 30*time.Millisecond, 0, 200*time.Millisecond)
	require.NoError(t, err)
	// Under {2, 3, 4}·BTT at BTT=30ms: budgets = [60, 90, 120, 200]ms;
	// T_broadcast_max per layer = [140, 110, 80, 0]ms. FetchAt must stay
	// ≤ T_broadcast_max for each layer.
	fetchAt = []time.Duration{
		100 * time.Millisecond, // L_0 (≤ 140)
		80 * time.Millisecond,  // L_1 (≤ 110)
		60 * time.Millisecond,  // L_2 (≤ 80)
		0,                      // L_3 deepest (B_3 = T_commit → T_broadcast_max_3 = 0)
	}
	return
}

type runnerNode struct {
	op    spectypes.OperatorID
	ctrl  *Controller
	sched *Scheduler
	hooks *runnerHooks
}

func (n *runnerNode) submittedOutput() *obftcore.Output {
	n.hooks.mu.Lock()
	defer n.hooks.mu.Unlock()
	if len(n.hooks.submitted) == 0 {
		return nil
	}
	return n.hooks.submitted[0]
}

func TestRunProposerSlot_Healthy_n4_K4(t *testing.T) {
	// Compressed timing for fast tests. The relative phase ordering matches
	// production but with smaller absolute durations.
	budget, fetchAt := compressedTestSchedule(t)
	overrides := &ConfigOverrides{
		K:               4,
		TCommit:         200 * time.Millisecond,
		Delta2:          60 * time.Millisecond, // 2·BTT_test — fixture is intentionally over-budgeted vs spec-recommended 1·BTT for sim stability
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	}

	nodes := buildCluster(t, 4, overrides)

	const slot = phase0.Slot(7)
	slotStart := time.Now()

	bus := newBroadcastBus(nodes, slotStart)
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			bus.broadcast(n.op, data)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	var ref *obftcore.Output
	for _, n := range nodes {
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output", n.op)
		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Value, out.Value), "op %d value differs", n.op)
		require.True(t, bytes.Equal(ref.Signature, out.Signature), "op %d signature differs", n.op)
		require.Equal(t, ref.Layer, out.Layer)
	}
	require.Equal(t, 0, ref.Layer, "healthy case should decide at L_0")
}

// TestRunProposerSlot_OpportunisticTiming_NoDelta2Wait verifies that with
// the observer-mode Phase-3 implementation (no Δ_2 pre-wait, no 25ms
// poller), submission lands before T_commit + Δ_2 in the healthy case.
// Under the previous schedule-anchored implementation, RunProposerSlot
// slept until T_commit + Δ_2 = 260ms before even starting to poll Resolve;
// observer-mode collapses that wait, so submission lands within a few BTTs
// of T_commit (well under Δ_2).
func TestRunProposerSlot_OpportunisticTiming_NoDelta2Wait(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	const tCommit = 200 * time.Millisecond
	const delta2 = 60 * time.Millisecond // 2·BTT_test (over-budgeted vs spec); old wait endpoint = TCommit+Δ_2 = 260ms
	overrides := &ConfigOverrides{
		K:               4,
		TCommit:         tCommit,
		Delta2:          delta2,
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	}

	nodes := buildCluster(t, 4, overrides)
	const slot = phase0.Slot(13)
	slotStart := time.Now()

	bus := newBroadcastBus(nodes, slotStart)
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			bus.broadcast(n.op, data)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	for _, n := range nodes {
		n.hooks.mu.Lock()
		require.NotEmptyf(t, n.hooks.submittedAt, "op %d submitted no output", n.op)
		elapsed := n.hooks.submittedAt[0].Sub(slotStart)
		n.hooks.mu.Unlock()
		// Submission must land before the old schedule-anchored wait endpoint
		// (T_commit + Δ_2 = 260ms). Healthy in-process bus has microsecond
		// dispatch, so the expected window is ~T_commit + a few ms.
		require.Lessf(t, elapsed, tCommit+delta2,
			"op %d submitted at %v after slot start; expected < T_commit+Δ_2 = %v "+
				"(observer-mode should fire on first σ-quorum, not wait the Δ_2 budget)",
			n.op, elapsed, tCommit+delta2)
	}
}

// TestRunProposerSlot_LateCommit_OpportunisticResolve verifies that the
// runner's Phase-3 polling salvages a slot where a peer's KindCommit is
// delayed past the soft target (RoundEndOffset = T_commit + Δ_2 + ε_3) but
// arrives before the slot's ctx deadline. Per spec §Phase 3 ("Re-running on
// late KindCommit arrivals"), an operator that initially can't reach σ-quorum
// can re-run the reconstruction walk when a late partial arrives — the
// behavior the new ResolveAndSubmitOpportunistically implements.
//
// Setup: 4 operators, K=4, healthy network. Two of the four KindCommits to
// op 1 are delayed past RoundEndOffset. With the OLD blocking-resolve at
// RoundEndOffset, op 1 would resolve once with only 2 partials (qV=3 not
// reached) and the slot would miss for op 1. With opportunistic polling, op
// 1 keeps retrying and eventually resolves successfully when the late
// commits arrive.
func TestRunProposerSlot_LateCommit_OpportunisticResolve(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	overrides := &ConfigOverrides{
		K:               4,
		TCommit:         200 * time.Millisecond,
		Delta2:          60 * time.Millisecond, // 2·BTT_test (over-budgeted vs spec); RoundEndOffset = TCommit+Δ_2+ε_3 = 320ms
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	}

	nodes := buildCluster(t, 4, overrides)
	const slot = phase0.Slot(11)
	slotStart := time.Now()

	// Late-Commit-targeted bus: KindCommit messages to victim op (op 1) from
	// senders 3 and 4 are delayed by lateCommitDelay (well past
	// RoundEndOffset = 320ms but well within the ctx 2s timeout).
	const lateCommitDelay = 500 * time.Millisecond
	const victim spectypes.OperatorID = 1
	bus := newBroadcastBusWithDelay(nodes, slotStart, func(from, to spectypes.OperatorID, kind byte) time.Duration {
		if kind == byte(wire.KindCommit) && to == victim && (from == 3 || from == 4) {
			return lateCommitDelay
		}
		return 0
	})
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			bus.broadcast(n.op, data)
			return nil
		}
	}

	// ctx deadline must be wide enough that the late commits arrive before
	// it fires (lateCommitDelay = 500ms ≪ 2s); observer-mode wakes on each
	// arrival via the state-delta channel and retries Resolve immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	// All operators (including the victim of late commits) must successfully
	// submit an Output. The victim's submission proves opportunistic Resolve
	// picked up the late commits past RoundEndOffset.
	var ref *obftcore.Output
	for _, n := range nodes {
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output (opportunistic Resolve failed for late-commit case)", n.op)
		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Value, out.Value), "op %d value differs", n.op)
		require.True(t, bytes.Equal(ref.Signature, out.Signature), "op %d signature differs", n.op)
	}
}

// TestScheduler_OpportunisticResolve_EndInstanceWhileWaiting verifies the
// risk-mitigation invariant from OBFT-OPPORTUNISTIC-PHASE3-PLAN.md §Risks:
// "The Controller's StateDeltaChan(slot) must close when EndInstance fires
// for that slot. Orphaned waiters in ResolveAndSubmitOpportunistically
// must unblock cleanly."
//
// Setup: build a cluster but only let ONE node start its instance. The
// node's Resolve will return ErrNoQuorum (own partials < qV); the
// scheduler enters its select-on-deltas loop with nothing to receive.
// We then call EndInstance externally, which closes the state-delta
// channel. The scheduler must (a) not panic from a send-on-closed-channel
// race (impossible by the Ended()-under-instanceMu pattern but the race
// detector validates), (b) detect the close and nil the deltas case, and
// (c) eventually return cleanly when ctx fires.
func TestScheduler_OpportunisticResolve_EndInstanceWhileWaiting(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	overrides := &ConfigOverrides{
		K:               4,
		TCommit:         200 * time.Millisecond,
		Delta2:          60 * time.Millisecond,
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	}
	nodes := buildCluster(t, 4, overrides)
	node := nodes[0]

	const slot = phase0.Slot(99)
	_, err := node.ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	// 1s ctx — generous upper bound on the unblock time. Actual return time
	// after EndInstance is dominated by ctx.Done() firing (the scheduler
	// nils the deltas case on close, leaving only ctx as the exit), so the
	// resolver waits the rest of the ctx after the external EndInstance.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Build own commit so the instance has self-observed partials in pool.
	// (Optimistic Resolve will still return ErrNoQuorum at qV=3 because
	// the cluster's other 3 ops haven't emitted; that's the point — the
	// scheduler then blocks on stateDelta.)
	_, err = node.sched.BuildAndBroadcastCommit(ctx, slot)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- node.sched.ResolveAndSubmitOpportunistically(ctx, slot)
	}()

	// Give the resolver time to enter its wait loop. Without this brief
	// sleep the EndInstance below could close the channel before the
	// resolver even reads from it, leaving the test less faithful to the
	// "resolver actively waiting → externally torn down" scenario.
	time.Sleep(50 * time.Millisecond)

	endStart := time.Now()
	node.ctrl.EndInstance(slot)

	// Resolver must return within ctx timeout + small slack. The race
	// detector validates that the channel close happened concurrently with
	// the receive without panic.
	const slack = 200 * time.Millisecond
	select {
	case err := <-done:
		elapsed := time.Since(endStart)
		require.Less(t, elapsed, 1*time.Second+slack,
			"resolver took %v to return after external EndInstance; expected ≤ ctx timeout + %v",
			elapsed, slack)
		// Returned error should reflect the ctx exit path. ErrNoActiveInstance
		// would also be acceptable (if a delta fire happened to coincide with
		// the close) but the typical path here is ctx.Done() → wrapped
		// ctx.Err().
		require.Error(t, err, "resolver should return an error after ctx fires")
		t.Logf("resolver returned %v after %v (external EndInstance + ctx unblock)", err, elapsed)
	case <-time.After(2 * time.Second):
		t.Fatal("resolver deadlocked after external EndInstance — channel-close handling broken")
	}
}

// ---- cluster setup helpers ----------------------------------------------

func buildCluster(t *testing.T, n int, overrides *ConfigOverrides) []*runnerNode {
	t.Helper()
	threshold.Init()
	f := (n - 1) / 3
	q := 2*f + 1

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubShares := make(map[obftcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[obftcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}

	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	nodes := make([]*runnerNode, 0, n)
	for _, op := range committee {
		opSigner := blsbackend.New(shares[uint64(op)].Serialize()) //nolint:unconvert

		ctrl, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{0xCA, 0xFE},
			ClusterPubKey: masterPub,
			PubKeyShares:  pubShares,
			Signer:        opSigner,
			IBE:           ibe,
			Overrides:     overrides,
		})
		require.NoError(t, err)

		hooks := newRunnerHooks(op)
		hooks.fetchFn = func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			if hooks.fetchErr != nil {
				return nil, hooks.fetchErr
			}
			return []byte(fmt.Sprintf("slot-%d-layer-%d-block", slot, layer)), nil
		}
		hooks.hostValidateFn = func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
			return true, nil
		}

		s, err := NewScheduler(ctrl, hooks.LifecycleHooks())
		require.NoError(t, err)

		nodes = append(nodes, &runnerNode{op: op, ctrl: ctrl, sched: s, hooks: hooks})
	}
	return nodes
}

// ---- broadcast bus -------------------------------------------------------

// delayFn returns the per-message delivery delay for a given (from → to,
// envelope kind) tuple. Returning 0 means deliver immediately. The kind byte
// is the wire.Kind value (KindPhase1Bundle / KindCommit / KindCertificate).
type delayFn func(from, to spectypes.OperatorID, kind byte) time.Duration

type broadcastBus struct {
	nodes     []*runnerNode
	slotStart time.Time
	delay     delayFn
	wg        sync.WaitGroup
	once      sync.Once
}

func newBroadcastBus(nodes []*runnerNode, slotStart time.Time) *broadcastBus {
	return &broadcastBus{nodes: nodes, slotStart: slotStart}
}

// newBroadcastBusWithDelay returns a bus that defers delivery per the
// supplied delayFn. Useful for late-Commit / late-Phase1Bundle scenarios
// that exercise the runner's opportunistic Resolve poll path.
func newBroadcastBusWithDelay(nodes []*runnerNode, slotStart time.Time, delay delayFn) *broadcastBus {
	return &broadcastBus{nodes: nodes, slotStart: slotStart, delay: delay}
}

func (b *broadcastBus) broadcast(from spectypes.OperatorID, data []byte) {
	observedOffset := time.Since(b.slotStart)
	// Peek at the envelope kind so the delay rule can dispatch on it
	// without each delivery goroutine reparsing.
	var kind byte
	if env, err := wire.Unwrap(data); err == nil && env != nil {
		kind = byte(env.Kind)
	}
	for _, n := range b.nodes {
		if n.op == from {
			continue
		}
		n := n
		dataCopy := append([]byte{}, data...)
		var d time.Duration
		if b.delay != nil {
			d = b.delay(from, n.op, kind)
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			if d > 0 {
				time.Sleep(d)
			}
			_ = DispatchBytes(context.Background(), n.sched, dataCopy, observedOffset)
		}()
	}
}

func (b *broadcastBus) stop() {
	b.once.Do(func() {
		b.wg.Wait()
	})
}

// ---- runnerHooks --------------------------------------------------------

type runnerHooks struct {
	op spectypes.OperatorID

	mu sync.Mutex

	fetchFn        func(context.Context, phase0.Slot, int) ([]byte, error)
	fetchErr       error
	hostValidateFn func(context.Context, phase0.Slot, int, []byte) (bool, error)
	broadcastFn    func(context.Context, phase0.Slot, []byte) error

	submitted   []*obftcore.Output
	submittedAt []time.Time
	missed      []phase0.Slot
}

func newRunnerHooks(op spectypes.OperatorID) *runnerHooks {
	return &runnerHooks{op: op}
}

func (h *runnerHooks) LifecycleHooks() *LifecycleHooks {
	return &LifecycleHooks{
		FetchCandidate: func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			h.mu.Lock()
			fn := h.fetchFn
			h.mu.Unlock()
			if fn == nil {
				return nil, fmt.Errorf("hooks: no FetchCandidate configured")
			}
			return fn(ctx, slot, layer)
		},
		HostValidate: func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
			h.mu.Lock()
			fn := h.hostValidateFn
			h.mu.Unlock()
			if fn == nil {
				return true, nil
			}
			return fn(ctx, slot, layer, value)
		},
		Broadcast: func(ctx context.Context, slot phase0.Slot, data []byte) error {
			h.mu.Lock()
			fn := h.broadcastFn
			h.mu.Unlock()
			if fn == nil {
				return nil
			}
			return fn(ctx, slot, data)
		},
		SubmitOutput: func(ctx context.Context, slot phase0.Slot, out *obftcore.Output) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.submitted = append(h.submitted, out)
			h.submittedAt = append(h.submittedAt, time.Now())
			return nil
		},
		OnMissedSlot: func(ctx context.Context, slot phase0.Slot, reason error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.missed = append(h.missed, slot)
		},
	}
}

// TestScheduler_IterativeFetch_PicksFreshest verifies the spec §Application
// iterative-fetch model: the Scheduler polls FetchCandidate throughout the
// available window and uses the last (freshest) successful result.
func TestScheduler_IterativeFetch_PicksFreshest(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	nodes := buildCluster(t, 4, &ConfigOverrides{
		K:               4,
		TCommit:         200 * time.Millisecond,
		Delta2:          60 * time.Millisecond,
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	})
	leader := nodes[0]

	// Configure leader's hook to return a different value on each call.
	var callCount int
	leader.hooks.mu.Lock()
	leader.hooks.fetchFn = func(_ context.Context, _ phase0.Slot, _ int) ([]byte, error) {
		leader.hooks.mu.Lock()
		callCount++
		v := []byte(fmt.Sprintf("v-%d", callCount))
		leader.hooks.mu.Unlock()
		return v, nil
	}
	leader.hooks.mu.Unlock()

	// Tight poll cadence so the test exercises multiple iterations within
	// a sub-second window. Build buffer kept tiny for the test fixture.
	require.NoError(t, leader.sched.SetFetchTiming(20*time.Millisecond, 5*time.Millisecond))

	// 200ms ctx deadline → with 20ms pollInterval and 5ms buildBuffer,
	// pollEnd is at +195ms; multiple polls fit.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(200*time.Millisecond))
	defer cancel()
	freshest, err := leader.sched.iterativeFetch(ctx, phase0.Slot(123), 0)
	require.NoError(t, err)

	leader.hooks.mu.Lock()
	finalCallCount := callCount
	leader.hooks.mu.Unlock()
	require.Greater(t, finalCallCount, 1, "expected multiple polls within window; got %d", finalCallCount)
	require.Equal(t, fmt.Sprintf("v-%d", finalCallCount), string(freshest),
		"freshest value should be the LAST poll's result")
}

// TestScheduler_IterativeFetch_DegradesToSingleShot verifies that with a
// short window (deadline already past pollEnd), iterativeFetch does a
// single fetch+return rather than spinning.
func TestScheduler_IterativeFetch_DegradesToSingleShot(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	nodes := buildCluster(t, 4, &ConfigOverrides{
		K:               4,
		TCommit:         200 * time.Millisecond,
		Delta2:          60 * time.Millisecond,
		Eps3:            60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	})
	leader := nodes[0]

	var callCount int
	leader.hooks.mu.Lock()
	leader.hooks.fetchFn = func(_ context.Context, _ phase0.Slot, _ int) ([]byte, error) {
		leader.hooks.mu.Lock()
		callCount++
		leader.hooks.mu.Unlock()
		return []byte("v"), nil
	}
	leader.hooks.mu.Unlock()

	// pollInterval larger than window → single fetch, immediate return.
	require.NoError(t, leader.sched.SetFetchTiming(500*time.Millisecond, 50*time.Millisecond))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()
	freshest, err := leader.sched.iterativeFetch(ctx, phase0.Slot(123), 0)
	require.NoError(t, err)
	require.Equal(t, "v", string(freshest))

	leader.hooks.mu.Lock()
	finalCallCount := callCount
	leader.hooks.mu.Unlock()
	require.Equal(t, 1, finalCallCount, "expected exactly 1 poll in single-shot degeneration; got %d", finalCallCount)
}
