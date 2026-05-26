package base

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Cluster simulator used by the protocol-level tests below.
//
// Models n operators, each owning a private *Instance. The simulator wires
// up Phase-1 bundle delivery (selectively, to test partition / equivocation
// scenarios), Phase-2 KindCommit exchange, and Phase-3 Resolve. Per-operator
// host validity verdicts are configurable to exercise the NV path.
//
// The simulator uses StubSigner / StubIBE so tests run without real BLS
// machinery; that's fine for protocol-level coverage (the cryptography is
// exercised separately in blsbackend tests once that package is migrated).

type sim struct {
	t            *testing.T
	n            int
	f            int
	K            int
	cfg          *Config
	pubKeyShares map[OperatorID][]byte
	instances    map[OperatorID]*Instance
	candidates   map[int]Value // canonical V per layer (used by helper paths)
}

// newSim builds a cluster of size n with K = min(4, n) layers (the OBFT
// proposer-duty default). Layer leaders rotate by op-id: layer k's leader
// is operator (k mod n) + 1. For K=2 simulations, use newSimWithK.
func newSim(t *testing.T, n int) *sim {
	t.Helper()
	K := 4
	if K > n {
		K = n
	}
	return newSimWithK(t, n, K)
}

// newSimWithK builds a cluster of size n with the caller-chosen K layers.
// Use this for K=f+1 (BFT-liveness minimum) tests; newSim defaults to K=4
// which is the SSV proposer-duty default.
func newSimWithK(t *testing.T, n, K int) *sim {
	t.Helper()
	f := (n - 1) / 3

	operators := make([]OperatorID, n)
	pubKeyShares := make(map[OperatorID][]byte, n)
	for i := 0; i < n; i++ {
		op := OperatorID(i + 1)
		operators[i] = op
		pubKeyShares[op] = []byte{byte(op)}
	}

	// Per-layer FetchAt: L_0 (primary) latest = 500ms; L_1..L_{K-1} (backups)
	// all fetch at slot start (FetchAt=0) per the simplified backup
	// schedule — B_k = T_commit for backups clamps T_broadcast_max_k to
	// BFT_start, so backup FetchAt must be 0.
	const btt = 150 * time.Millisecond // P99=100 + δ=50 fixture
	const tCommit = 1500 * time.Millisecond
	// SafetyBuffer=0 in sim tests: idealized eager-push delivery; mesh-reflood
	// scenarios are exercised via test-specific schedules, not via the static
	// reflood-absorption budget.
	budgets, err := DefaultBroadcastBudget(K, btt, 0, tCommit)
	require.NoError(t, err)
	layers := make([]LayerSpec, K)
	for k := 0; k < K; k++ {
		var fetchAt time.Duration
		if k == 0 {
			fetchAt = 500 * time.Millisecond
		} // backups (k>0) and degenerate K=1 case both leave fetchAt = 0
		layers[k] = LayerSpec{
			Leader:          operators[k%n],
			FetchAt:         fetchAt,
			BroadcastBudget: budgets[k],
		}
	}

	cfg := &Config{
		Height:    100,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: operators,
		F:         f,
		Layers:    layers,
		TCommit:   tCommit,
		Delta2:    300 * time.Millisecond,
		Eps3:      250 * time.Millisecond,
		BTT:       btt,
	}
	require.NoError(t, cfg.Validate())

	qv := cfg.QV()
	instances := make(map[OperatorID]*Instance, n)
	for _, op := range operators {
		signer := NewStubSigner(qv, []byte{byte(op)})
		ibe := NewStubIBE(qv)
		inst, err := NewInstance(cfg, op, signer, signer, ibe, []byte{0xCC, 0xDD}, pubKeyShares, nil, nil)
		require.NoError(t, err)
		instances[op] = inst
	}

	candidates := make(map[int]Value, K)
	for k := 0; k < K; k++ {
		candidates[k] = []byte(fmt.Sprintf("layer-%d-canonical-V", k))
	}

	return &sim{
		t: t, n: n, f: f, K: K,
		cfg:          cfg,
		pubKeyShares: pubKeyShares,
		instances:    instances,
		candidates:   candidates,
	}
}

// newSimWithStaggeredBudgets builds a cluster with per-layer BroadcastBudget
// set to a custom staggered test fixture (B_0=0.5·BTT, B_1=1·BTT, B_2=2·BTT,
// B_3=5·BTT) — exercises the per-layer-budget mechanism end-to-end. The
// current spec schedule is primary-vs-backup (B_0 = 2·BTT + SafetyBuffer;
// B_1..B_{K-1} = T_commit per [docs/OBFT.md §Setting](OBFT.md)); these
// staggered ratios are a non-canonical test fixture, not spec-recommended.
func newSimWithStaggeredBudgets(t *testing.T, n int) *sim {
	t.Helper()
	s := newSim(t, n)
	btt := s.cfg.BTT // 150ms with the test fixture
	// Bump TCommit so B_3 = 5·BTT fits.
	s.cfg.TCommit = 5*btt + 100*time.Millisecond
	budgets := []time.Duration{btt / 2, btt, 2 * btt, 5 * btt}
	for k := 0; k < s.K; k++ {
		s.cfg.Layers[k].BroadcastBudget = budgets[k]
		s.cfg.Layers[k].FetchAt = s.cfg.TCommit - budgets[k]
	}
	require.NoError(t, s.cfg.Validate())
	return s
}

// leaderAt returns the layer-k leader.
func (s *sim) leaderAt(layer int) OperatorID {
	return s.cfg.Layers[layer].Leader
}

// allOperators returns all operator IDs in the cluster.
func (s *sim) allOperators() []OperatorID {
	out := make([]OperatorID, len(s.cfg.Operators))
	copy(out, s.cfg.Operators)
	return out
}

// deliverPhase1 has the layer's leader build a Phase-1 bundle on `value`,
// then delivers it to each operator in `recipients`. Each recipient calls
// ObservePhase1Bundle + (optionally) ApplyHostValidity.
func (s *sim) deliverPhase1(layer int, value Value, recipients []OperatorID, observedOffset time.Duration, hostValid bool) {
	s.t.Helper()
	leaderID := s.leaderAt(layer)
	leaderInst := s.instances[leaderID]
	bundle, err := leaderInst.BuildPhase1Bundle(layer, value)
	require.NoError(s.t, err)
	// Apply host validity at the leader's own instance too — they need to
	// know the bundle they emitted is valid (in steady-state this is a
	// no-op since they already chose the V before fetching).
	require.NoError(s.t, leaderInst.ApplyHostValidity(layer, value, hostValid))

	for _, rcp := range recipients {
		if rcp == leaderID {
			continue // leader already self-observed via BuildPhase1Bundle
		}
		inst := s.instances[rcp]
		require.NoError(s.t, inst.ObservePhase1Bundle(bundle, observedOffset))
		require.NoError(s.t, inst.ApplyHostValidity(layer, value, hostValid))
	}
}

// deliverPhase1Equivocation has the layer's leader broadcast TWO distinct
// bundles (V_a, V_b) to disjoint sets of recipients. Used to model leader
// equivocation patterns. Both bundles are accepted-and-validated by the
// receivers.
//
// NOTE: we model the equivocation via the Phase1Bundle path's BuildPhase1Bundle.
// EKM-side, BuildPhase1Bundle locks σ-V on the first call's V; a second
// call with a different V would be rejected by the leader's own EKM. To
// model a *byzantine* leader who bypasses EKM, we construct the second
// bundle by calling SignPartial directly (sidestepping the EKM lock).
func (s *sim) deliverPhase1Equivocation(layer int, vA, vB Value, recipientsA, recipientsB []OperatorID, observedOffset time.Duration, hostValid bool) {
	s.t.Helper()
	leaderID := s.leaderAt(layer)
	leaderInst := s.instances[leaderID]

	// First bundle: legitimate path locks the EKM.
	bundleA, err := leaderInst.BuildPhase1Bundle(layer, vA)
	require.NoError(s.t, err)
	require.NoError(s.t, leaderInst.ApplyHostValidity(layer, vA, hostValid))

	// Second bundle: bypass EKM by signing directly (modeling a byzantine
	// leader who does not enforce single-σ-V on themselves).
	signer := NewStubSigner(s.cfg.QV(), []byte{byte(leaderID)})
	sigB, err := signer.SignPartial(vB)
	require.NoError(s.t, err)
	bundleB := &Phase1Bundle{
		ClusterID:   s.cfg.ClusterID,
		OperatorID:  leaderID,
		Height:      s.cfg.Height,
		Layer:       layer,
		Value:       append(Value{}, vB...),
		LeaderSigma: sigB,
	}

	for _, rcp := range recipientsA {
		if rcp == leaderID {
			continue
		}
		inst := s.instances[rcp]
		require.NoError(s.t, inst.ObservePhase1Bundle(bundleA, observedOffset))
		require.NoError(s.t, inst.ApplyHostValidity(layer, vA, hostValid))
	}
	for _, rcp := range recipientsB {
		if rcp == leaderID {
			continue
		}
		inst := s.instances[rcp]
		require.NoError(s.t, inst.ObservePhase1Bundle(bundleB, observedOffset))
		require.NoError(s.t, inst.ApplyHostValidity(layer, vB, hostValid))
	}
}

// runPhase2 has every operator build their KindCommit and gossip it to all
// peers. Each operator observes every peer's commit before Resolve.
//
// `excludeFrom` is a set of operators whose Phase-2 messages should NOT be
// broadcast (modelling offline / silent-byzantine operators).
func (s *sim) runPhase2(excludeFrom map[OperatorID]bool) {
	s.t.Helper()

	// Phase 2: each operator emits its KindCommit at TCommit. Per spec,
	// each operator commits exactly once per slot, carrying both σ partials
	// and NR partials in a single message.
	commits := make(map[OperatorID]*Commit)
	for op, inst := range s.instances {
		if excludeFrom[op] {
			continue
		}
		c, err := inst.BuildOwnCommit()
		require.NoError(s.t, err)
		commits[op] = c
	}

	// Each operator observes every peer's commit (idempotent on duplicate
	// observation).
	for receiver, inst := range s.instances {
		if excludeFrom[receiver] {
			continue
		}
		for sender, c := range commits {
			if sender == receiver {
				continue
			}
			require.NoError(s.t, inst.ObserveCommit(c))
		}
	}
}

// resolveAll runs Resolve on every (non-excluded) instance and returns the
// outputs keyed by operator. Errors (including ErrNoQuorum) yield a nil
// entry for that operator.
func (s *sim) resolveAll(excludeFrom map[OperatorID]bool) map[OperatorID]*Output {
	out := make(map[OperatorID]*Output, len(s.instances))
	for op, inst := range s.instances {
		if excludeFrom[op] {
			continue
		}
		o, err := inst.Resolve()
		if err != nil {
			out[op] = nil
			continue
		}
		out[op] = o
	}
	return out
}

// requireAllAgree asserts that every non-nil output decided the same (Layer,
// Value).
func requireAllAgree(t *testing.T, outputs map[OperatorID]*Output) *Output {
	t.Helper()
	var canonical *Output
	for op, o := range outputs {
		if o == nil {
			continue
		}
		if canonical == nil {
			canonical = o
			continue
		}
		require.Equal(t, canonical.Layer, o.Layer, "op %d disagreed on layer", op)
		require.True(t, bytes.Equal(canonical.Value, o.Value), "op %d disagreed on value", op)
	}
	require.NotNil(t, canonical, "no operator produced an output")
	return canonical
}

// requireAllReconstruct asserts that every non-excluded operator independently
// reached σ-quorum locally — stronger than requireAllAgree, which would pass
// if only the layer leader reconstructed and the rest fell back to certificate
// gossip. Used to catch M1-style local-pool gaps.
func requireAllReconstruct(t *testing.T, outputs map[OperatorID]*Output) *Output {
	t.Helper()
	var canonical *Output
	for op, o := range outputs {
		require.NotNilf(t, o, "op %d failed to reconstruct locally", op)
		if canonical == nil {
			canonical = o
			continue
		}
		require.Equalf(t, canonical.Layer, o.Layer, "op %d disagreed on layer", op)
		require.Truef(t, bytes.Equal(canonical.Value, o.Value), "op %d disagreed on value", op)
	}
	require.NotNil(t, canonical, "no operator produced an output")
	return canonical
}
