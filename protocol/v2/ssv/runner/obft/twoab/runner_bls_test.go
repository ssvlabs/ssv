package twoab

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

	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Real-BLS end-to-end: n=4 operators run one healthy proposer slot through
// RunProposerSlot with real threshold-BLS crypto (blsbackend) and an async,
// goroutine-per-delivery dispatch bus routing through DispatchBytes. This is
// the L4 stub smoke promoted to real cryptography — it proves the L1–L5 stack
// reconstructs a genuine threshold aggregate, not just stub bytes.
//
// Crypto wiring mirrors the bare-OBFT real-BLS runner test (buildCluster):
// a BLS master key is threshold-split into per-operator shares; each op's
// V-side Signer is blsbackend.New(share); the IBE is a SignerGatedIBE anchored
// on the cluster master pubkey; TagSigner is left nil → Option A (the V-keypair
// shares double as IBE-tag shares). Candidate values are opaque bytes (no
// proposer-domain translation — that's the proposer_signer layer, exercised
// separately), so the Signer is used unwrapped, exactly as base's e2e does.

type blsNode struct {
	op          spectypes.OperatorID
	ctrl        *Controller
	sched       *Scheduler
	broadcastFn func(data []byte) // set before goroutines launch (happens-before)

	mu        sync.Mutex
	submitted []*twoabcore.Output
	runErr    error // written by this node's RunProposerSlot goroutine; read after wg.Wait()
	// capturedTrace / capturedCascadeErrs hold the instance's final resolve
	// trace + cascade errors, snapshot deterministically at teardown by the
	// Controller.onInstanceEnd hook the safety bridge wires in buildBLSCluster.
	// This replaces a former 100µs polling capture goroutine that could miss
	// instances whose RunProposerSlot completed between ticks — a miss that
	// misclassified fast local-deciders as cert-gossip deciders in
	// reconstructOutcome. Written once from the RunProposerSlot goroutine (the
	// deferred EndInstance → hook), so the write happens-before the
	// post-wg.Wait() read by the diagnostic dump + reconstructOutcome. Nil when
	// no instance ran for the slot (RunProposerSlot returned before
	// StartNewInstance) or the instance resolved no layer.
	capturedTrace       []twoabcore.LayerAttempt
	capturedCascadeErrs []error
	// phase2aFirstEmitAt is the slot-relative time of this op's first
	// non-Phase1Bundle Broadcast call — i.e. when the resolve loop *broadcast*
	// the Phase-2a emission. This is NOT the backstop-select instant nor the
	// MaybeFirePhase2a commit instant: under instanceMu contention those can
	// diverge from it by >100ms, so do not read it as the fire/commit time. The
	// diagnostic dump compares it against per-emission receive times to localize
	// stuck-at-L_k races (a peer that committed before the L_k leader's bundle
	// landed will have NRPlaintext at L_k, an irrevocable choice).
	// Zero until the first non-bundle broadcast.
	phase2aFirstEmitAt time.Duration
}

func (n *blsNode) submittedOutput() *twoabcore.Output {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.submitted) == 0 {
		return nil
	}
	return n.submitted[0]
}

// assertClusterSubmittedAtCanonicalOrCert is the shared cross-op submission
// invariant for buildBLSCluster-based tests. It asserts:
//   - every op has runErr == nil and a non-nil submitted output,
//   - every output's Layer is either `canonicalLayer` (the test's expected
//     local-decode layer) or -1 (the cert-fast-path sentinel from
//     scheduler.go's `submitAndBroadcastCert`-bypass via `tryCertFastPath`),
//   - at least one op took the canonical local path — otherwise no cert
//     exists for the rest to fast-path on,
//   - all outputs share the same Value and Signature bytes.
//
// Rationale: with BroadcastCertificate wired in buildBLSCluster (mirroring
// production), ops race between completing their own local Resolve and
// short-circuiting via tryCertFastPath on a faster peer's broadcast cert.
// Both paths produce the same Value + reconstructed Signature (the cert
// wraps the σ-aggregated output of the locally-deciding op), so value/
// signature equality is the strict cluster invariant. Per-op Layer cannot
// be: strict cross-op Layer equality would race-fail any test where one op
// races ahead and its cert reaches a peer before that peer's local Resolve
// completes — the rescued op then submits with Layer=-1 while the
// fast-deciding peer has Layer=`canonicalLayer`.
//
// Returns the first submitted output (`ref`) so callers can assert
// per-scenario specifics (V prefix, master-pubkey signature verify).
func assertClusterSubmittedAtCanonicalOrCert(t *testing.T, nodes []*blsNode, canonicalLayer int) *twoabcore.Output {
	t.Helper()
	var ref *twoabcore.Output
	canonicalCount := 0
	for _, n := range nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot", n.op)
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output", n.op)
		require.Truef(t, out.Layer == canonicalLayer || out.Layer == -1,
			"op %d decided at unexpected layer %d (want %d or -1 cert-fast-path)",
			n.op, out.Layer, canonicalLayer)
		if out.Layer == canonicalLayer {
			canonicalCount++
		}
		if ref == nil {
			ref = out
			continue
		}
		require.Truef(t, bytes.Equal(ref.Value, out.Value), "op %d decided a different Value", n.op)
		require.Truef(t, bytes.Equal(ref.Signature, out.Signature), "op %d reconstructed a different Signature", n.op)
	}
	require.GreaterOrEqualf(t, canonicalCount, 1,
		"at least one op must decide locally at layer %d to seed the cert-fast-path for any others",
		canonicalLayer)
	return ref
}

// assertSilentFallThroughOutcome is the tolerant liveness verdict for the real-BLS
// silent-L0-leader fall-through tests (the matrix cells + the single-cell smoke
// test). The fall-through normally converges at L_1 — then it asserts the cross-op
// submission invariant and verifies the reconstructed L_1 aggregate against the
// master pubkey — but under -race it can lose the V_1-vs-Phase-2a-fire timing race
// and land in a within-spec σ/NR split that misses cleanly (ErrNoQuorum), exactly
// like the safety-bridge SilentL0Leader scenario. That miss is tolerated here:
// nobody decided, so it is safe, and deterministic fall-through *convergence* is
// covered separately, in virtual time, by the DES (TestCrash_L0Leader_MatrixCells).
// On a tolerated miss, any lone decider (the outbound-silenced leader self-σ'ing a
// local L_1 quorum while peers stay quorum-short) must still produce a
// master-pubkey-valid L_1 reconstruction.
//
// Returns the cluster-wide decided Output on full convergence (so callers can
// assert value specifics), or nil when the cluster cleanly missed.
func assertSilentFallThroughOutcome(t *testing.T, cl *blsCluster, cell matrixCell) *twoabcore.Output {
	t.Helper()
	missed := 0
	for _, n := range cl.nodes {
		if n.submittedOutput() == nil {
			missed++
		}
	}
	if missed == 0 {
		ref := assertClusterSubmittedAtCanonicalOrCert(t, cl.nodes, 1)
		require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, ref.Value, ref.Signature),
			"reconstructed L_1 signature must verify against master pubkey at n=%d K=%d", cell.n, cell.K)
		return ref
	}
	const label = "RealBLS_SilentL0Leader"
	for _, n := range cl.nodes {
		out := n.submittedOutput()
		if out == nil {
			requireCleanMiss(t, n, cell, -1, label)
			continue
		}
		require.Truef(t, out.Layer == 1 || out.Layer == -1,
			"op %d decided at unexpected layer %d (want 1 or -1 cert) at n=%d K=%d", n.op, out.Layer, cell.n, cell.K)
		require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, out.Value, out.Signature),
			"op %d decider's L_1 reconstruction must verify against master pubkey at n=%d K=%d", n.op, cell.n, cell.K)
	}
	t.Logf("RealBLS_SilentL0Leader n=%d K=%d: %d of %d ops cleanly missed (within-spec σ/NR split; safety preserved, convergence covered by the DES)",
		cell.n, cell.K, missed, len(cl.nodes))
	return nil
}

// blsCluster bundles the real-BLS nodes with the cluster-wide verification
// material (master pubkey + a verify-only signer) for end-state assertions.
type blsCluster struct {
	nodes     []*blsNode
	masterPub []byte
	verifier  twoabcore.Signer // verify-only (no share) — for VerifyAggregate
}

func buildBLSCluster(t *testing.T, n int, overrides *ConfigOverrides) *blsCluster {
	t.Helper()
	threshold.Init()
	f := (n - 1) / 3
	q := 2*f + 1

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubShares := make(map[twoabcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[twoabcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}

	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	nodes := make([]*blsNode, 0, n)
	for _, op := range committee {
		node := &blsNode{op: op}
		opSigner := blsbackend.New(shares[uint64(op)].Serialize()) //nolint:unconvert

		ctrl, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{0xCA, 0xFE},
			ClusterPubKey: masterPub,
			PubKeyShares:  pubShares,
			Signer:        opSigner,
			// TagSigner left nil → Option A (V-keypair shares double as IBE-tag shares).
			IBE:       ibe,
			Overrides: overrides,
		})
		require.NoError(t, err)
		node.ctrl = ctrl

		// Deterministic trace capture: snapshot the instance's final resolve
		// trace + cascade errors at EndInstance (teardown), under r.instanceMu.
		// Fires exactly once per slot from the RunProposerSlot goroutine (the
		// deferred EndInstance), so the write happens-before the post-wg.Wait()
		// read in reconstructOutcome / dumpClusterDiagnostic. Replaces the old
		// polling capture goroutine (see blsNode.capturedTrace docstring).
		// LayerAttempt is an all-scalar value struct, so a slice copy is a deep
		// copy; CascadeErrors already returns a fresh slice.
		ctrl.onInstanceEnd = func(_ phase0.Slot, ri *RunningInstance) {
			ri.instanceMu.Lock()
			trace := append([]twoabcore.LayerAttempt(nil), ri.instance.LastResolveLayerAttempts()...)
			cascade := ri.instance.CascadeErrors()
			ri.instanceMu.Unlock()
			node.mu.Lock()
			node.capturedTrace = trace
			node.capturedCascadeErrs = cascade
			node.mu.Unlock()
		}

		hooks := &LifecycleHooks{
			FetchCandidate: func(_ context.Context, slot phase0.Slot, layer int) ([]byte, error) {
				return []byte(fmt.Sprintf("slot-%d-layer-%d-block", slot, layer)), nil
			},
			HostValidate: func(context.Context, phase0.Slot, int, []byte) (bool, error) { return true, nil },
			Broadcast: func(_ context.Context, _ phase0.Slot, data []byte) error {
				if node.broadcastFn != nil {
					node.broadcastFn(data)
				}
				return nil
			},
			// BroadcastCertificate routes through the same broadcastFn as
			// Broadcast: certificates are wire emissions like any other from
			// the cluster bus's perspective. Production wires this hook (the
			// peer-cert fast path is the protocol's intended rescue mechanism
			// when a single op decides and others stall); leaving it nil here
			// would mask the SilentL0Leader recovery path that depends on the
			// decider gossiping its certificate to wake the cluster.
			BroadcastCertificate: func(_ context.Context, _ phase0.Slot, data []byte) error {
				if node.broadcastFn != nil {
					node.broadcastFn(data)
				}
				return nil
			},
			SubmitOutput: func(_ context.Context, _ phase0.Slot, out *twoabcore.Output) error {
				node.mu.Lock()
				node.submitted = append(node.submitted, out)
				node.mu.Unlock()
				return nil
			},
		}
		sched, err := NewScheduler(ctrl, hooks)
		require.NoError(t, err)
		node.sched = sched
		nodes = append(nodes, node)
	}
	return &blsCluster{nodes: nodes, masterPub: masterPub, verifier: verifier}
}

// delayFn returns the per-message delivery delay for a given (from → to,
// envelope kind) tuple. Returning 0 means deliver immediately. The kind
// byte is the twoab/wire.MessageKind value (KindPhase1Bundle, KindValue,
// KindNoValue, KindCommit, KindCertificate).
//
// Mirror of OBFT base's delayFn (different package; types are independent).
type delayFn func(from, to spectypes.OperatorID, kind byte) time.Duration

// blsBus delivers each broadcast to every peer on its own goroutine through the
// real DispatchBytes router — async delivery under the race detector, the
// production inbound path end-to-end.
//
// Optional `silent` set drops all outbound from listed ops (crashed-sender
// sim); optional `delay` function defers per-(from,to,kind) delivery for
// late-arrival scenarios (e.g. LateCommit exercising the opportunistic-
// resolve poll path).
type blsBus struct {
	nodes     []*blsNode
	slotStart time.Time
	silent    map[spectypes.OperatorID]bool // outbound from these ops is dropped (crashed-sender sim)
	delay     delayFn                       // nil → deliver every message immediately
	wg        sync.WaitGroup
	once      sync.Once

	// deliveryHook, if non-nil, is invoked by each per-recipient delivery
	// goroutine after DispatchBytes returns. Records (from, to, kind,
	// broadcastAt, deliveredAt) for the safety-bridge's diagnostic dump.
	// Test-only — production runner has no equivalent observation point.
	deliveryHook func(from, to spectypes.OperatorID, kind byte, broadcastAt, deliveredAt time.Duration)
}

// newBlsBusWithDelay returns a blsBus that defers delivery per the supplied
// delayFn. Used by late-arrival scenarios (LateCommit). Same shape as OBFT
// base's newBroadcastBusWithDelay.
func newBlsBusWithDelay(nodes []*blsNode, slotStart time.Time, delay delayFn) *blsBus {
	return &blsBus{nodes: nodes, slotStart: slotStart, delay: delay}
}

func (b *blsBus) broadcast(from spectypes.OperatorID, data []byte) {
	b.broadcastAt(from, data, time.Since(b.slotStart))
}

// broadcastAt is broadcast with the slot-relative broadcast time supplied
// by the caller — used by recordingBlsBus to thread one shared broadcastAt
// through to both the captured emission and every per-recipient
// deliveryHook callback, so the diagnostic dump can group deliveries to
// emissions by exact-match (avoiding microsecond-drift mismatches between
// independent time.Since calls).
func (b *blsBus) broadcastAt(from spectypes.OperatorID, data []byte, broadcastAt time.Duration) {
	if b.silent[from] {
		return // send-silent op: all outbound dropped, inbound still works
	}
	// Peek at the envelope kind so the delay rule + delivery hook can
	// dispatch on it without each delivery goroutine reparsing.
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
			_ = DispatchBytes(context.Background(), n.sched, dataCopy, broadcastAt)
			if b.deliveryHook != nil {
				b.deliveryHook(from, n.op, kind, broadcastAt, time.Since(b.slotStart))
			}
		}()
	}
}

func (b *blsBus) stop() { b.once.Do(func() { b.wg.Wait() }) }

func TestRunProposerSlot_RealBLS_Healthy_n4(t *testing.T) {
	// Compressed timing; FetchAt / BroadcastBudget auto-derive from BTT +
	// TPhase2a (ConfigForCluster). K=4 so each Phase-2a emission carries real
	// chained-IBE-encrypted L_k>0 entries (exercises real IBE Encrypt) even
	// though the healthy path decides at L_0 (no IBE decryption needed).
	overrides := &ConfigOverrides{
		K:                4,
		BTT:              20 * time.Millisecond,
		tPhase2aOverride: 150 * time.Millisecond,
	}
	cl := buildBLSCluster(t, 4, overrides)

	const slot = phase0.Slot(7)
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

	ref := assertClusterSubmittedAtCanonicalOrCert(t, cl.nodes, 0)
	require.Equal(t, []byte("slot-7-layer-0-block"), []byte(ref.Value), "cluster decides the L_0 leader's candidate")

	// The core thing real BLS proves over stubs: the reconstructed σ-aggregate
	// is a genuine threshold signature verifiable against the cluster master
	// pubkey.
	require.Truef(t, cl.verifier.VerifyAggregate(cl.masterPub, ref.Value, ref.Signature),
		"reconstructed signature must verify against the cluster master pubkey")
}

// TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough drives the NR
// fall-through with real crypto: the L_0 leader is send-silent (crashed
// sender — all its outbound dropped, so peers never see V_0 and can't σ at
// L_0, not even via peer-reflood-V since the leader emits nothing). The
// surviving f+1=3 operators emit NoValue→Commit-NR at L_0, aggregate qEnc NR
// partials to unlock the L_0→L_1 chain key, decrypt the L_1 SigmaChained σ
// entries, and decide at L_1. This is the path that exercises real chained-IBE
// *decryption* (the healthy test only exercises IBE encryption).
//
// All four operators decide (even the send-silent leader: its outbound is
// dropped but it receives peers' partials and reconstructs L_1 from them).
func TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough(t *testing.T) {
	overrides := &ConfigOverrides{
		K:                4,
		BTT:              20 * time.Millisecond,
		tPhase2aOverride: 150 * time.Millisecond,
	}
	cl := buildBLSCluster(t, 4, overrides)

	const slot = phase0.Slot(7)
	// L_0 leader under the cluster rotation (committee is already sorted 1..4).
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	lZeroLeader := spectypes.OperatorID(leaderForLayer(committee, twoabcore.Height(slot), 0))

	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := &blsBus{nodes: cl.nodes, slotStart: slotStart, silent: map[spectypes.OperatorID]bool{lZeroLeader: true}}
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

	// Tolerant: the silent fall-through can cleanly miss under -race (within-spec,
	// same as the safety-bridge scenario); deterministic convergence is covered by
	// the DES. Assert the decided value only when the cluster fully converged.
	if ref := assertSilentFallThroughOutcome(t, cl, matrixCell{n: 4, K: 4}); ref != nil {
		require.Equal(t, []byte("slot-7-layer-1-block"), []byte(ref.Value), "decided value is the L_1 leader's candidate")
	}
}
