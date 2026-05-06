package obft

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
// scenarios), Phase-2 onion + NR exchange, and Phase-3 Resolve. Per-operator
// host validity verdicts are configurable to exercise the NV / Defer paths.
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
// is operator (k mod n) + 1.
func newSim(t *testing.T, n int) *sim {
	t.Helper()
	f := (n - 1) / 3
	K := 4
	if K > n {
		K = n
	}
	if K < 3 {
		t.Fatalf("OBFT requires K >= 3; cluster size n=%d only supports K=%d", n, K)
	}

	operators := make([]OperatorID, n)
	pubKeyShares := make(map[OperatorID][]byte, n)
	for i := 0; i < n; i++ {
		op := OperatorID(i + 1)
		operators[i] = op
		pubKeyShares[op] = []byte{byte(op)}
	}

	// Per-layer FetchAt: T_{K-1} earliest, T_0 latest. Use 50ms decrements
	// so the schedule is monotonically decreasing in k. All within the
	// broadcast deadline for the timing config below.
	layers := make([]LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = LayerSpec{
			Leader:  operators[k%n],
			FetchAt: 1100*time.Millisecond - time.Duration(k)*50*time.Millisecond,
		}
	}

	cfg := &Config{
		Height:    100,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: operators,
		F:         f,
		Layers:    layers,
		TCommit:   1500 * time.Millisecond,
		Delta2:    300 * time.Millisecond,
		Delta3:    250 * time.Millisecond,
		D:         100 * time.Millisecond,
		Delta:     50 * time.Millisecond,
	}
	require.NoError(t, cfg.Validate())

	qv := cfg.QV()
	instances := make(map[OperatorID]*Instance, n)
	for _, op := range operators {
		signer := NewStubSigner(qv, []byte{byte(op)})
		ibe := NewStubIBE(qv)
		inst, err := NewInstance(cfg, op, signer, signer, ibe, []byte{0xCC, 0xDD}, pubKeyShares, nil)
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
		OperatorID: leaderID,
		Height:     s.cfg.Height,
		Layer:      layer,
		Value:      append(Value{}, vB...),
		SigmaV:     sigB,
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

// runPhase2 has every operator build their KindOnion + KindNR (after
// PhaseTwoEnd) and gossip them to all peers. Each operator observes every
// peer's payload before Resolve.
//
// `excludeFrom` is a set of operators whose Phase-2 messages should NOT be
// broadcast (modelling offline / silent-byzantine operators).
func (s *sim) runPhase2(excludeFrom map[OperatorID]bool) {
	s.t.Helper()

	// Phase 2: each operator emits its KindOnion immediately at TCommit
	// (since we don't model the late-σ-emit path here unless tests do so
	// explicitly via direct Phase-1 timing).
	onions := make(map[OperatorID]*Onion)
	for op, inst := range s.instances {
		if excludeFrom[op] {
			continue
		}
		o, err := inst.BuildOwnOnion()
		require.NoError(s.t, err)
		onions[op] = o
	}

	// Each operator observes every peer's onion (including their own —
	// idempotent on duplicate observation).
	for receiver, inst := range s.instances {
		if excludeFrom[receiver] {
			continue
		}
		for sender, o := range onions {
			if sender == receiver {
				continue
			}
			require.NoError(s.t, inst.ObserveOnion(o))
		}
	}

	// End of Phase 2 → BuildOwnNR for everyone.
	nrs := make(map[OperatorID]*NR)
	for op, inst := range s.instances {
		if excludeFrom[op] {
			continue
		}
		require.NoError(s.t, inst.PhaseTwoEnd())
		nr, err := inst.BuildOwnNR()
		require.NoError(s.t, err)
		nrs[op] = nr
	}

	// Each operator observes every peer's NR.
	for receiver, inst := range s.instances {
		if excludeFrom[receiver] {
			continue
		}
		for sender, nr := range nrs {
			if sender == receiver {
				continue
			}
			require.NoError(s.t, inst.ObserveNR(nr))
		}
	}

	// Late σ-emits: in case any operator transitioned to σ during
	// PhaseTwoEnd (Defer-partition resolved), they need to broadcast a
	// fresh KindOnion. In our deterministic simulator, the σ-emit window
	// has already passed, but we re-broadcast for the σ-pool reconstruction
	// to pick up the partial. Spec allows this — late σ-emits enter the
	// Phase 3 σ-pool even if peers already locked NR.
	for op, inst := range s.instances {
		if excludeFrom[op] {
			continue
		}
		o, err := inst.BuildOwnOnion()
		require.NoError(s.t, err)
		for receiver, recvInst := range s.instances {
			if excludeFrom[receiver] || receiver == op {
				continue
			}
			require.NoError(s.t, recvInst.ObserveOnion(o))
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
