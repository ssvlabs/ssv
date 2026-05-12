package twoab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test scaffolding shared across twoab/ unit tests. Mirrors base/sim_test.go
// in shape; the simulator will grow as Phases F-H land (BuildVerdict /
// ObserveVerdict / BuildOnion2b / ObserveOnion2b / Resolve hooks).
//
// At Phase E, the sim helper builds a fully-wired Instance per operator
// using the StubSigner / StubIBE stubs so tests run without real BLS
// machinery. Cryptographic correctness is exercised separately in
// blsbackend tests.

// observedEarly is a Phase-1-bundle first-observation offset comfortably
// within the accept-eligible window (well before T_accept_max).
// Healthy-config T_verdict_start = 1.6s, T_accept_max = 1.8s, T_commit = 2.0s.
const observedEarly = 1200 * time.Millisecond

// observedAuthOnly is past T_accept_max (1.8s) but before T_commit (2.0s)
// — bundle goes into auth-only retention.
const observedAuthOnly = 1900 * time.Millisecond

// observedLate is past T_commit — bundle is rejected with
// ErrLatePhase1Bundle.
const observedLate = 2100 * time.Millisecond

// sim is a minimal multi-operator harness for protocol-level tests.
type sim struct {
	t         *testing.T
	n, f, K   int
	cfg       *Config
	pubShares map[OperatorID][]byte
	instances map[OperatorID]*Instance

	// canonical Phase-1 V's per layer (populated by tests as needed).
	candidates map[int]Value
}

// newSim builds an n-operator cluster at f=1, K=min(4, n) with stub
// signers + IBE wired into every Instance. Layer leaders rotate by op ID
// (L_0 leader = op1, L_1 = op2, ..., L_{K-1} = op_K), modeling the
// simplest layer assignment.
func newSim(t *testing.T, n int) *sim {
	return newSimWithF(t, n, 1)
}

// newSimWithF is the F-parameterized variant of newSim, used by Phase-J
// n=7/f=2 scenarios that exercise the validity-divergence majority claim
// at larger cluster sizes. Uses the SSV proposer-duty default K=min(4, n)
// (capped at n for small clusters); for K=f+1 BFT-liveness-minimum tests
// at f=1, see newSimWithK.
func newSimWithF(t *testing.T, n, f int) *sim {
	K := 4
	if n < K {
		K = n
	}
	return newSimWithFK(t, n, f, K)
}

// newSimWithK builds an f=1 sim with the caller-chosen K (≥ f+1 = 2).
// Use for K=2 (BFT-liveness minimum) tests at the default n=4.
func newSimWithK(t *testing.T, n, K int) *sim {
	return newSimWithFK(t, n, 1, K)
}

// newSimWithFK is the underlying constructor — explicit n, f, K.
func newSimWithFK(t *testing.T, n, f, K int) *sim {
	t.Helper()
	require.GreaterOrEqual(t, n, 3*f+1, "sim requires n >= 3f+1")
	require.GreaterOrEqual(t, K, f+1, "sim requires K >= f+1 (BFT-liveness minimum)")
	require.LessOrEqual(t, K, n, "sim requires K <= n")

	c := healthyConfig()
	// Adjust the cluster shape if it differs from healthyConfig (n=4, f=1, K=4).
	if n != 4 || f != 1 || K != 4 {
		ops := make([]OperatorID, n)
		layers := make([]LayerSpec, K)
		btt := 200 * time.Millisecond
		tCommit := 2000 * time.Millisecond
		tVerdictStart := tCommit - 400*time.Millisecond
		budgets, err := DefaultBroadcastBudget(K, btt, tVerdictStart)
		require.NoError(t, err)
		for k := 0; k < n; k++ {
			ops[k] = OperatorID(k + 1)
		}
		for k := 0; k < K; k++ {
			layers[k] = LayerSpec{
				Leader:          ops[k],
				FetchAt:         tVerdictStart - budgets[k],
				BroadcastBudget: budgets[k],
			}
			if k == K-1 {
				layers[k].FetchAt = 0
			}
		}
		c = &Config{
			Height:    c.Height,
			ClusterID: c.ClusterID,
			Operators: ops,
			F:         f,
			Layers:    layers,
			TCommit:   tCommit,
			Delta2a:   400 * time.Millisecond,
			Delta2b:   400 * time.Millisecond,
			Delta3:    250 * time.Millisecond,
			BTT:       btt,
		}
	}
	require.NoError(t, c.Validate())

	// Build a stub-signer-friendly pubkey-share map: one share per
	// operator, where the share is derived deterministically from the
	// operator's ID (so verification works against a known shared key).
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
			nil, // clusterPubKey — stub VerifyAggregate ignores it
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
// different subsets of recipients. Returns both built bundles for
// further use.
func (s *sim) deliverPhase1Equivocation(
	layer int,
	vA, vB Value,
	recipientsA, recipientsB []OperatorID,
	observedOffset time.Duration,
) (bA, bB *Phase1Bundle) {
	s.t.Helper()
	leader := s.leaderAt(layer)
	var err error
	bA, err = s.instances[leader].BuildPhase1Bundle(layer, vA)
	require.NoError(s.t, err)
	bB, err = s.instances[leader].BuildPhase1Bundle(layer, vB)
	require.NoError(s.t, err)
	for _, op := range recipientsA {
		require.NoError(s.t, s.instances[op].ObservePhase1Bundle(bA, observedOffset))
	}
	for _, op := range recipientsB {
		require.NoError(s.t, s.instances[op].ObservePhase1Bundle(bB, observedOffset))
	}
	return bA, bB
}

// applyHostValidityAll applies the host's valid/not-valid verdict for V
// at layer across every Instance in the sim. Used in Phase F tests to
// set up the "host says valid/not-valid" branch of ComputeLocalVerdict.
func (s *sim) applyHostValidityAll(layer int, value Value, valid bool) {
	s.t.Helper()
	for _, op := range s.allOperators() {
		require.NoError(s.t, s.instances[op].ApplyHostValidity(layer, value, valid))
	}
}

// applyHostValidityFor applies the host's verdict on only the listed
// operators (modeling validity-divergence: some honest see V as valid,
// others don't).
func (s *sim) applyHostValidityFor(ops []OperatorID, layer int, value Value, valid bool) {
	s.t.Helper()
	for _, op := range ops {
		require.NoError(s.t, s.instances[op].ApplyHostValidity(layer, value, valid))
	}
}

// deliverVerdict has `from` build a Verdict at layer and deliver it to
// every recipient via ObserveVerdict. Returns the built verdict.
func (s *sim) deliverVerdict(from OperatorID, layer int, recipients []OperatorID) *Verdict {
	s.t.Helper()
	v, err := s.instances[from].BuildVerdict(layer)
	require.NoError(s.t, err, "op %d BuildVerdict at layer %d", from, layer)
	for _, op := range recipients {
		require.NoError(s.t, s.instances[op].ObserveVerdict(v),
			"op %d ObserveVerdict from %d at layer %d", op, from, layer)
	}
	return v
}

// deliverVerdictEquivocation has a byzantine operator broadcast TWO
// distinct verdicts at the same layer (different Kinds or different
// ValueRoots), selectively delivering each to different subsets of
// recipients. Useful for Rule-6a / treat-as-null tests.
//
// Both verdicts are constructed directly (bypassing BuildVerdict's
// idempotent cache) since this models a byzantine that doesn't honor
// the single-emission rule.
func (s *sim) deliverVerdictEquivocation(
	from OperatorID,
	layer int,
	vA, vB *Verdict,
	recipientsA, recipientsB []OperatorID,
) {
	s.t.Helper()
	for _, op := range recipientsA {
		require.NoError(s.t, s.instances[op].ObserveVerdict(vA),
			"op %d ObserveVerdict A from %d at layer %d", op, from, layer)
	}
	for _, op := range recipientsB {
		require.NoError(s.t, s.instances[op].ObserveVerdict(vB),
			"op %d ObserveVerdict B from %d at layer %d", op, from, layer)
	}
}

// runPhase2aCanonical has every operator BuildVerdict + broadcast at
// each layer in [0, K). Returns a slice of all broadcast verdicts so
// tests can inspect them.
func (s *sim) runPhase2aCanonical() []*Verdict {
	s.t.Helper()
	var out []*Verdict
	for k := 0; k < s.K; k++ {
		for _, op := range s.allOperators() {
			v, err := s.instances[op].BuildVerdict(k)
			require.NoError(s.t, err, "op %d BuildVerdict layer %d", op, k)
			out = append(out, v)
			for _, peer := range s.allOperators() {
				if peer == op {
					continue
				}
				require.NoError(s.t, s.instances[peer].ObserveVerdict(v))
			}
		}
	}
	return out
}

// runPhase2bCanonical has every operator BuildOwnOnion2b and broadcasts
// to every other operator. Returns the slice of all built onions in
// operator-ID order.
func (s *sim) runPhase2bCanonical() []*Onion2b {
	s.t.Helper()
	var out []*Onion2b
	for _, op := range s.allOperators() {
		o, err := s.instances[op].BuildOwnOnion2b()
		require.NoError(s.t, err, "op %d BuildOwnOnion2b", op)
		out = append(out, o)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(s.t, s.instances[peer].ObserveOnion2b(o),
				"op %d ObserveOnion2b from %d", peer, op)
		}
	}
	return out
}

// withObserver replaces s.instances[op] with a fresh Instance wired to
// the supplied EvidenceObserver. The replacement Instance shares the
// sim's config + pub-share map but starts with empty state, so callers
// should call withObserver before any Observe* / Apply* on that op.
//
// Used by Phase-I evidence-observer tests to assert one-fire-per-
// (Rule, OperatorID, Layer) semantics without rebuilding the cluster.
func (s *sim) withObserver(op OperatorID, observer EvidenceObserver) {
	s.t.Helper()
	inst, err := NewInstance(
		s.cfg, op,
		NewStubSigner(s.cfg.QV(), s.pubShares[op]),
		NewStubSigner(s.cfg.QV(), s.pubShares[op]),
		NewStubIBE(s.cfg.QEnc()),
		nil, s.pubShares, nil, observer,
	)
	require.NoError(s.t, err)
	s.instances[op] = inst
}

// resolveAll calls Resolve on every Instance in the sim and returns
// per-op outputs. Errors are returned in the second map; tests assert
// against either the output map or the error map as appropriate.
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
// agrees on the same (Layer, Value) pair. Returns the canonical output
// for the test to assert against.
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
