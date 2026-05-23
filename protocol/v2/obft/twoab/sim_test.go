package twoab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test scaffolding shared across twoab/ unit tests. Each test sets up a
// 4-op (or 7-op) cluster with stub signers / IBE, drives the protocol
// through Phase 1 → Phase 2a → Phase 2b → Phase 3, and asserts the
// expected outcome.

// observedEarly is a Phase-1-bundle first-observation offset comfortably
// within the slot (well before T_phase_2a). Healthy-config TPhase2a = 1000ms.
const observedEarly = 600 * time.Millisecond

// observedAfterPhase2a is past TPhase2a but well within the slot —
// models a late-arriving bundle that may still drive the upgrade.
const observedAfterPhase2a = 1500 * time.Millisecond

// sim is a multi-operator harness for protocol-level tests.
type sim struct {
	t         *testing.T
	n, f, K   int
	cfg       *Config
	pubShares map[OperatorID][]byte
	instances map[OperatorID]*Instance

	// canonical Phase-1 V's per layer (populated by tests as needed).
	candidates map[int]Value
}

// newSim builds an n-operator cluster at f=1, K=2 (BFT-liveness minimum
// at n=4 — the recommended SSV default) with stub signers + IBE.
func newSim(t *testing.T, n int) *sim {
	return newSimWithFK(t, n, 1, 2)
}

// newSimWithK builds an f=1 sim with the caller-chosen K (≥ f+1 = 2).
func newSimWithK(t *testing.T, n, K int) *sim {
	return newSimWithFK(t, n, 1, K)
}

// newSimWithFK is the underlying constructor — explicit n, f, K.
func newSimWithFK(t *testing.T, n, f, K int) *sim {
	t.Helper()
	require.GreaterOrEqual(t, n, 3*f+1, "sim requires n >= 3f+1")
	require.GreaterOrEqual(t, K, f+1, "sim requires K >= f+1 (BFT-liveness minimum)")
	require.LessOrEqual(t, K, n, "sim requires K <= n")

	const (
		btt          = 200 * time.Millisecond
		tPhase2a     = 1000 * time.Millisecond
		safetyBuffer = 0 // direct-delivery tests don't need cascade cushion
	)
	t0Broadcast := tPhase2a - btt

	ops := make([]OperatorID, n)
	for k := 0; k < n; k++ {
		ops[k] = OperatorID(k + 1)
	}
	budgets, err := DefaultBroadcastBudget(K, btt, t0Broadcast)
	require.NoError(t, err)

	layers := make([]LayerSpec, K)
	for k := 0; k < K; k++ {
		fetchAt := t0Broadcast - budgets[k]
		if fetchAt < 0 {
			fetchAt = 0
		}
		layers[k] = LayerSpec{
			Leader:          ops[k],
			FetchAt:         fetchAt,
			BroadcastBudget: budgets[k],
		}
	}
	c := &Config{
		Height:       1,
		ClusterID:    [32]byte{0xaa, 0xbb, 0xcc},
		Operators:    ops,
		F:            f,
		Layers:       layers,
		TPhase2a:     tPhase2a,
		SafetyBuffer: safetyBuffer,
		BTT:          btt,
		BFTStart:     0,
	}
	require.NoError(t, c.Validate())

	pubShares := make(map[OperatorID][]byte, n)
	for _, op := range c.Operators {
		pubShares[op] = []byte{byte(op)}
	}

	candidates := make(map[int]Value, K)
	for k := 0; k < K; k++ {
		candidates[k] = Value("L" + string(rune('0'+k)) + "-V")
	}

	s := &sim{
		t:          t,
		n:          n,
		f:          f,
		K:          K,
		cfg:        c,
		pubShares:  pubShares,
		instances:  make(map[OperatorID]*Instance, n),
		candidates: candidates,
	}
	for _, op := range c.Operators {
		inst, err := NewInstance(
			c, op,
			NewStubSigner(c.QV(), pubShares[op]),
			NewStubSigner(c.QV(), pubShares[op]),
			NewStubIBE(c.QEnc()),
			[]byte{0xCC, 0xDD}, // clusterPubKey — stub VerifyAggregate ignores the value; non-empty placeholder for NewInstance's emptiness check
			pubShares,
			nil, // ibePubKeyShares — Option A
			nil, // evidenceObserver — set per test as needed
		)
		require.NoError(t, err)
		s.instances[op] = inst
	}
	return s
}

// allOperators returns the cluster's full operator set.
func (s *sim) allOperators() []OperatorID {
	out := make([]OperatorID, len(s.cfg.Operators))
	copy(out, s.cfg.Operators)
	return out
}

// leaderAt returns the designated leader at layer k.
func (s *sim) leaderAt(layer int) OperatorID {
	return s.cfg.Layers[layer].Leader
}

// l0Witness wraps a single L_0 forwarded leader witness on v into the
// Witnesses[] shape a valid KindValue requires. Test convenience for
// the common case where a ValueMsg only forwards the mandatory L_0 witness.
func l0Witness(v Value, sig Signature) []LayerWitness {
	return []LayerWitness{{Layer: 0, ValueRoot: ValueRoot(v), Witness: sig}}
}

// l0WitnessOf extracts the L_0 forwarded witness signature from a ValueMsg's
// Witnesses[] (test convenience; nil if absent).
func l0WitnessOf(vm *ValueMsg) Signature {
	for _, w := range vm.Witnesses {
		if w.Layer == 0 {
			return w.Witness
		}
	}
	return nil
}

// deliverPhase1 has the leader at `layer` build a bundle for `value`,
// then delivers it (via ObservePhase1Bundle at the given offset) to each
// of `recipients` (which may include the leader for self-observe).
func (s *sim) deliverPhase1(layer int, value Value, recipients []OperatorID, observedOffset time.Duration) *Phase1Bundle {
	s.t.Helper()
	leader := s.leaderAt(layer)
	b, err := s.instances[leader].BuildPhase1Bundle(layer, value)
	require.NoError(s.t, err)
	for _, op := range recipients {
		err := s.instances[op].ObservePhase1Bundle(b, observedOffset)
		require.NoError(s.t, err, "op %d ObservePhase1Bundle", op)
	}
	return b
}

// deliverPhase1Equivocation has the (presumed-byzantine) leader build
// TWO distinct bundles V_a and V_b and selectively deliver them to
// different subsets of recipients.
//
// The legitimate BuildPhase1Bundle path acquires σ-lock at L_0 on the
// emitter's instance. Equivocation is inherently byz behavior — the
// byz leader bypasses EKM to sign on multiple V's. Both bundles here
// use the test-only direct signer access (buildByzEquivocatingBundle),
// so the byz leader's own instance remains EKM-unlocked and can later
// take any path (typically NRDirect once they observe their own
// equivocation via self-observation).
func (s *sim) deliverPhase1Equivocation(
	layer int,
	vA, vB Value,
	recipientsA, recipientsB []OperatorID,
	observedOffset time.Duration,
) (bA, bB *Phase1Bundle) {
	s.t.Helper()
	leader := s.leaderAt(layer)
	bA = s.buildByzEquivocatingBundle(leader, layer, vA)
	bB = s.buildByzEquivocatingBundle(leader, layer, vB)
	for _, op := range recipientsA {
		require.NoError(s.t, s.instances[op].ObservePhase1Bundle(bA, observedOffset))
	}
	for _, op := range recipientsB {
		require.NoError(s.t, s.instances[op].ObservePhase1Bundle(bB, observedOffset))
	}
	return bA, bB
}

// buildByzEquivocatingBundle constructs a second Phase-1 bundle from
// `leader` on a different value `vB` — bypassing the EKM σ-lock that
// would otherwise block a legitimate second BuildPhase1Bundle call.
// Used by equivocation tests to model a byz leader who signs σ partials
// on multiple V's (which the BLS signer permits; only the protocol's
// EKM gate blocks honest paths).
//
// The LWitness is signed via direct signer access at every layer (the
// layer leader's witness is required at all layers). Equivocation
// tests model a byz leader who signs σ partials on multiple V's, which the
// BLS signer permits — only the protocol's EKM gate blocks honest paths.
func (s *sim) buildByzEquivocatingBundle(leader OperatorID, layer int, vB Value) *Phase1Bundle {
	s.t.Helper()
	inst := s.instances[leader]
	w, err := inst.signer.SignPartial(vB)
	require.NoError(s.t, err)
	return &Phase1Bundle{
		ClusterID:  inst.cfg.ClusterID,
		OperatorID: leader,
		Height:     inst.cfg.Height,
		Layer:      layer,
		Value:      append(Value{}, vB...),
		LWitness:   w,
	}
}

// applyHostValidityAll applies the host's valid/not-valid verdict for V
// at layer across every Instance in the sim.
func (s *sim) applyHostValidityAll(layer int, value Value, valid bool) {
	s.t.Helper()
	for _, op := range s.allOperators() {
		require.NoError(s.t, s.instances[op].ApplyHostValidity(layer, value, valid))
	}
}

// applyHostValidityFor applies the host's verdict on only the listed
// operators (modeling validity-divergence).
func (s *sim) applyHostValidityFor(ops []OperatorID, layer int, value Value, valid bool) {
	s.t.Helper()
	for _, op := range ops {
		require.NoError(s.t, s.instances[op].ApplyHostValidity(layer, value, valid))
	}
}

// firePhase2aAll fires Phase 2a on every operator and cross-broadcasts
// the resulting emissions (ValueMsg / NoValueMsg / Commit-NRDirect) to
// every other operator. The post-fire afterStateDelta cascade inside
// each Observe* method automatically drives commit-trigger evaluation
// and upgrade-check propagation, so this single call advances the
// cluster through Phase 2a + Phase 2b.
func (s *sim) firePhase2aAll() {
	s.t.Helper()
	// First pass: fire Phase 2a on every op. Collect emissions.
	type emission struct {
		op OperatorID
		vm *ValueMsg
		nv *NoValueMsg
		c  *Commit
	}
	emissions := make([]emission, 0, len(s.instances))
	for _, op := range s.allOperators() {
		vm, nv, c, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(s.t, err, "op %d MaybeFirePhase2a", op)
		emissions = append(emissions, emission{op: op, vm: vm, nv: nv, c: c})
	}
	// Cross-broadcast each emission to all peers.
	for _, e := range emissions {
		for _, peer := range s.allOperators() {
			if peer == e.op {
				continue
			}
			switch {
			case e.vm != nil:
				require.NoError(s.t, s.instances[peer].ObserveValueMsg(e.vm),
					"op %d ObserveValueMsg from %d", peer, e.op)
			case e.nv != nil:
				require.NoError(s.t, s.instances[peer].ObserveNoValueMsg(e.nv),
					"op %d ObserveNoValueMsg from %d", peer, e.op)
			case e.c != nil:
				require.NoError(s.t, s.instances[peer].ObserveCommit(e.c),
					"op %d ObserveCommit from %d (NRDirect)", peer, e.op)
			}
		}
	}
	// Second pass: collect any Phase-2b Commits or Phase-2a-late upgrade
	// KindValues that the cascade emitted, and cross-broadcast those too.
	s.propagatePostPhase2aEmissions()
}

// propagatePostPhase2aEmissions cross-broadcasts any post-Phase-2a
// emissions (upgrade KindValues, Phase-2b Commits) that the afterStateDelta
// cascade produced during firePhase2aAll. Repeats until no new emissions
// fire (since each cross-broadcast may trigger further cascading).
func (s *sim) propagatePostPhase2aEmissions() {
	s.t.Helper()
	// Track which emissions we've already broadcast (by op).
	upgraded := make(map[OperatorID]bool)
	committed := make(map[OperatorID]bool)
	const maxRounds = 16 // defensive — convergence is finite
	for round := 0; round < maxRounds; round++ {
		progress := false
		for _, op := range s.allOperators() {
			inst := s.instances[op]
			// Check for new upgrade KindValue.
			if vm, ok := inst.OwnValueMsg(); ok && !upgraded[op] {
				// Only an upgrade if op also has ownNoValueMsg (the NoValue→Value path).
				if _, hadNV := inst.OwnNoValueMsg(); hadNV {
					upgraded[op] = true
					progress = true
					for _, peer := range s.allOperators() {
						if peer == op {
							continue
						}
						require.NoError(s.t, s.instances[peer].ObserveValueMsg(vm),
							"op %d ObserveValueMsg upgrade from %d", peer, op)
					}
				}
			}
			// Check for new Phase-2b Commit (Side=NR, not NRDirect
			// — NRDirect was already broadcast in firePhase2aAll).
			if c, ok := inst.OwnCommit(); ok && !committed[op] && c.Side != CommitSideNRDirect {
				committed[op] = true
				progress = true
				for _, peer := range s.allOperators() {
					if peer == op {
						continue
					}
					require.NoError(s.t, s.instances[peer].ObserveCommit(c),
						"op %d ObserveCommit from %d", peer, op)
				}
			}
			// NRDirect emissions were captured during firePhase2aAll;
			// mark them committed here so we don't re-broadcast.
			if c, ok := inst.OwnCommit(); ok && !committed[op] && c.Side == CommitSideNRDirect {
				committed[op] = true
			}
		}
		if !progress {
			return
		}
	}
}

// resolveAll calls Resolve on every Instance in the sim and returns
// per-op outputs.
func (s *sim) resolveAll() (map[OperatorID]*Output, map[OperatorID]error) {
	s.t.Helper()
	outputs := make(map[OperatorID]*Output, len(s.cfg.Operators))
	errs := make(map[OperatorID]error, len(s.cfg.Operators))
	for _, op := range s.allOperators() {
		out, err := s.instances[op].Resolve()
		outputs[op] = out
		errs[op] = err
	}
	return outputs, errs
}

// requireAllAgree asserts that every (non-nil) output across operators
// agrees on the same (Layer, Value) pair. Returns the canonical output.
func requireAllAgree(t *testing.T, outputs map[OperatorID]*Output) *Output {
	t.Helper()
	var first *Output
	for op, out := range outputs {
		require.NotNil(t, out, "op %d reached no output", op)
		if first == nil {
			first = out
			continue
		}
		require.Equal(t, first.Layer, out.Layer, "op %d disagrees on layer", op)
		require.Equal(t, first.Value, out.Value, "op %d disagrees on value", op)
	}
	return first
}
