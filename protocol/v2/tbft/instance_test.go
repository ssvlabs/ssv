package tbft

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Protocol-level tests covering scenarios from docs/TBFT-comparison.md:
//
//   1. Healthy — all honest operators receive layer 0's value, layer 0
//      reaches quorum.
//   2. Top leader silent — layer 0 has no candidate, 2f+1 non-receipts
//      unlock layer 1, layer 1 succeeds.
//   3. Top leader byzantine equivocating — different operators see
//      different layer-0 values; no layer-0 quorum reachable; protocol
//      falls through to layer 1.
//   4. f operators offline — within byzantine bound, layer 0 still
//      reaches quorum.
//   5. Beyond byzantine bound — neither positive nor negative quorum at
//      any layer; ErrNoQuorum.
//   6. Inconsistency-fault detection — operator emits both positive sig
//      and non-receipt for same layer; recorded as a fault.
//
// The tests use a cluster simulator (see helpers below) that runs K
// candidates through the full pipeline (Phase 1 observation → Phase 2
// onion construction + non-receipt broadcast → Phase 3 resolve) and
// verifies all honest operators converge on the same Output.

// ---- Healthy scenarios ---------------------------------------------------

func TestResolve_Healthy_n7(t *testing.T) {
	sim := newSim(t, 7)
	sim.allOperatorsSeeAllCandidates()
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)
	require.NotNil(t, outputs[1])
	require.Equal(t, 0, outputs[1].Layer, "should decide at layer 0 in healthy case")
	require.True(t, bytes.Equal(sim.candidates[0], outputs[1].Value))
}

func TestResolve_Healthy_n4(t *testing.T) {
	sim := newSim(t, 4)
	sim.allOperatorsSeeAllCandidates()
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)
	require.Equal(t, 0, outputs[1].Layer)
}

// ---- Top leader silent ---------------------------------------------------

func TestResolve_TopLeaderSilent_n7(t *testing.T) {
	sim := newSim(t, 7)
	// No operator sees layer 0's value; all see layer 1 and layer 2.
	for op := OperatorID(1); op <= 7; op++ {
		sim.operatorSees(op, 1, sim.candidates[1])
		sim.operatorSees(op, 2, sim.candidates[2])
	}
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)
	require.Equal(t, 1, outputs[1].Layer, "should decide at layer 1 when layer 0 silent")
	require.True(t, bytes.Equal(sim.candidates[1], outputs[1].Value))
}

func TestResolve_TopLeaderSilent_n4(t *testing.T) {
	sim := newSim(t, 4)
	for op := OperatorID(1); op <= 4; op++ {
		sim.operatorSees(op, 1, sim.candidates[1])
	}
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)
	require.Equal(t, 1, outputs[1].Layer, "TBFT2: layer 0 silent → backup at layer 1 succeeds")
}

// ---- Byzantine leader equivocating --------------------------------------

func TestResolve_LayerZeroEquivocation_n7(t *testing.T) {
	// f=2 byzantine leader at layer 0 equivocates: sends V_a to operators
	// 1..3 and V_b to operators 4..7. The protocol's response is for each
	// honest operator who DETECTS this disagreement (during Phase 1 by
	// observing peers' Phase-1 broadcasts) to treat layer 0 as non-receipt:
	// don't contribute a positive sig, emit a non-receipt attestation.
	//
	// In the simulator we model the post-detection state directly: every
	// operator has no candidate at layer 0, and emits a non-receipt for
	// layer 0. The resolution then walks to layer 1 via the non-receipt
	// quorum, exactly as if the leader had been silent.
	//
	// This is the same protocol-level outcome as TestResolve_TopLeaderSilent_n7 —
	// from TBFT's perspective, equivocation and silence at a layer look
	// identical once the equivocation has been detected and treated as
	// non-receipt by honest operators.
	sim := newSim(t, 7)
	for op := OperatorID(1); op <= 7; op++ {
		// Layer 0 intentionally NOT visible to any operator (equivocation
		// detected; treated as non-receipt). Layers 1 and 2 are visible.
		sim.operatorSees(op, 1, sim.candidates[1])
		sim.operatorSees(op, 2, sim.candidates[2])
	}
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)
	require.Equal(t, 1, outputs[1].Layer,
		"layer 0 equivocation (treated as non-receipt) → fall through to layer 1")
}

// ---- f operators offline within bound -----------------------------------

func TestResolve_FOperatorsOffline_n7(t *testing.T) {
	// f=2 operators offline (3 and 7). Remaining 5 honest operators all
	// see all candidates. Layer 0 still has 5 = q sigs.
	sim := newSim(t, 7)
	sim.markOffline(3, 7)
	for op := OperatorID(1); op <= 7; op++ {
		if sim.isOffline(op) {
			continue
		}
		sim.operatorSees(op, 0, sim.candidates[0])
		sim.operatorSees(op, 1, sim.candidates[1])
		sim.operatorSees(op, 2, sim.candidates[2])
	}
	outputs := sim.runAll(t)
	requireAllAgreeAmongOnline(t, outputs, sim)
	for _, out := range outputs {
		if out == nil {
			continue
		}
		require.Equal(t, 0, out.Layer)
	}
}

// ---- Beyond byzantine bound ---------------------------------------------

func TestResolve_BeyondBound_n7(t *testing.T) {
	// 3 offline (> f=2). Only 4 operators contribute. Quorum = 5 cannot
	// be reached at any layer. All resolve calls return ErrNoQuorum.
	sim := newSim(t, 7)
	sim.markOffline(5, 6, 7)
	for op := OperatorID(1); op <= 7; op++ {
		if sim.isOffline(op) {
			continue
		}
		sim.operatorSees(op, 0, sim.candidates[0])
		sim.operatorSees(op, 1, sim.candidates[1])
		sim.operatorSees(op, 2, sim.candidates[2])
	}
	outputs := sim.runAll(t)
	for op, out := range outputs {
		if sim.isOffline(op) {
			continue
		}
		require.Nil(t, out, "op %d should not produce output beyond bound", op)
	}
	for _, err := range sim.lastErrors {
		require.True(t, errors.Is(err, ErrNoQuorum), "expected ErrNoQuorum, got %v", err)
	}
}

// ---- Inconsistency fault detection --------------------------------------

func TestResolve_InconsistencyFault(t *testing.T) {
	// Operator 1 emits both a positive sig at layer 0 AND a non-receipt
	// for layer 0. Provably contradictory — recorded as a fault.
	sim := newSim(t, 7)
	sim.allOperatorsSeeAllCandidates()
	// Op 1 also broadcasts a non-receipt for layer 0 even though they
	// have a layer-0 contribution in their onion.
	sim.alsoBroadcastNonReceipt(1, 0)
	outputs := sim.runAll(t)
	requireAllAgree(t, outputs)

	// Inspect any one operator's faults — they all see the same network.
	faults := sim.instances[2].InconsistencyFaults()
	require.Len(t, faults, 1)
	require.Equal(t, OperatorID(1), faults[0].OperatorID)
	require.Equal(t, 0, faults[0].Layer)
}

// ---- Test simulator ------------------------------------------------------

// sim is a per-test in-memory cluster simulator. It runs all operators in
// the same goroutine and lets each one independently build/observe/resolve.
type sim struct {
	cfg          *Config
	clusterPK    []byte
	signer       *StubSigner
	ibe          *StubIBE
	operators    []OperatorID
	candidates   []Value // candidate values per layer (cluster-wide canonical view)
	pubKeyShares map[OperatorID][]byte

	// per-(operator, layer) candidate visibility
	visible map[OperatorID]map[int]Value
	// operators marked offline (don't produce or contribute)
	offline map[OperatorID]bool
	// operators that should also broadcast non-receipt at given layer
	// even if they have a positive contribution
	alsoNR map[OperatorID]map[int]bool

	// post-runAll: each operator's instance + last result
	instances  map[OperatorID]*Instance
	lastErrors map[OperatorID]error
}

func newSim(t *testing.T, n int) *sim {
	t.Helper()
	cfg := validProposerConfig(t, n)
	q := cfg.Quorum()

	K := cfg.K()
	candidates := make([]Value, K)
	for k := 0; k < K; k++ {
		candidates[k] = Value(fmt.Sprintf("layer-%d-canonical-block", k))
	}

	pubKeyShares := make(map[OperatorID][]byte, n)
	for _, op := range cfg.Operators {
		// Stub convention: operator's pubkey share is the same single byte
		// as their secret-share (stub signer doesn't have a real keypair).
		pubKeyShares[op] = []byte{byte(op)}
	}

	s := &sim{
		cfg:          cfg,
		clusterPK:    []byte("cluster-pubkey"),
		signer:       NewStubSigner(q),
		ibe:          NewStubIBE(q),
		operators:    cfg.Operators,
		candidates:   candidates,
		pubKeyShares: pubKeyShares,
		visible:      make(map[OperatorID]map[int]Value),
		offline:      make(map[OperatorID]bool),
		alsoNR:       make(map[OperatorID]map[int]bool),
		instances:    make(map[OperatorID]*Instance),
		lastErrors:   make(map[OperatorID]error),
	}
	return s
}

func (s *sim) allOperatorsSeeAllCandidates() {
	for _, op := range s.operators {
		for k := 0; k < s.cfg.K(); k++ {
			s.operatorSees(op, k, s.candidates[k])
		}
	}
}

func (s *sim) operatorSees(op OperatorID, layer int, value Value) {
	if s.visible[op] == nil {
		s.visible[op] = make(map[int]Value)
	}
	s.visible[op][layer] = append(Value{}, value...)
}

func (s *sim) markOffline(ops ...OperatorID) {
	for _, op := range ops {
		s.offline[op] = true
	}
}

func (s *sim) isOffline(op OperatorID) bool {
	return s.offline[op]
}

func (s *sim) alsoBroadcastNonReceipt(op OperatorID, layer int) {
	if s.alsoNR[op] == nil {
		s.alsoNR[op] = make(map[int]bool)
	}
	s.alsoNR[op][layer] = true
}

// runAll simulates the full protocol: each operator builds an onion and any
// non-receipt attestations, all messages are gossiped to all (online)
// operators, then each operator runs Resolve. Returns each operator's
// Output (nil if Resolve returned an error).
func (s *sim) runAll(t *testing.T) map[OperatorID]*Output {
	t.Helper()

	// Each operator builds their own Instance, onion, and non-receipts.
	type produced struct {
		onion       *Onion
		nonReceipts []*NonReceiptAttestation
	}
	produces := make(map[OperatorID]*produced)

	for _, op := range s.operators {
		if s.offline[op] {
			continue
		}

		share := []byte{byte(op)} // stub: share = operator-id-byte

		// Build instance for this operator.
		inst, err := NewInstance(s.cfg, s.signer, s.ibe, s.clusterPK, s.pubKeyShares)
		require.NoError(t, err)
		s.instances[op] = inst

		// Phase 1: register own visible candidates with this operator's instance.
		for layer, v := range s.visible[op] {
			require.NoError(t, inst.ObserveCandidate(layer, v))
		}

		// Phase 2: build onion + non-receipts using the Instance's API.
		onion, err := inst.BuildOwnOnion(op, share)
		require.NoError(t, err)

		nrs, err := inst.BuildOwnNonReceipts(op, share)
		require.NoError(t, err)

		// Inject any "force NR" attestations on top of the standard
		// BuildOwnNonReceipts output (used to test inconsistency-fault
		// detection: operator emits a non-receipt for a layer where they
		// also contributed positively).
		for layer := 0; layer < s.cfg.K()-1; layer++ {
			if !s.alsoNR[op][layer] {
				continue
			}
			if _, has := s.visible[op][layer]; !has {
				// Already covered by BuildOwnNonReceipts; skip duplicate.
				continue
			}
			extra, err := BuildNonReceipt(s.cfg, op, share, layer, s.signer)
			require.NoError(t, err)
			nrs = append(nrs, extra)
		}

		produces[op] = &produced{onion: onion, nonReceipts: nrs}
	}

	// Gossip phase: every (online) operator's instance observes every
	// other (online) operator's onion + non-receipts.
	for _, recv := range s.operators {
		if s.offline[recv] {
			continue
		}
		for _, sender := range s.operators {
			if s.offline[sender] {
				continue
			}
			require.NoError(t, s.instances[recv].ObserveOnion(produces[sender].onion))
			for _, nr := range produces[sender].nonReceipts {
				require.NoError(t, s.instances[recv].ObserveNonReceipt(nr))
			}
		}
	}

	// Phase 3: each operator resolves independently.
	outputs := make(map[OperatorID]*Output)
	for _, op := range s.operators {
		if s.offline[op] {
			continue
		}
		out, err := s.instances[op].Resolve()
		s.lastErrors[op] = err
		if err == nil {
			outputs[op] = out
		}
	}
	return outputs
}

// requireAllAgree asserts that every operator's Output is identical.
func requireAllAgree(t *testing.T, outputs map[OperatorID]*Output) {
	t.Helper()
	require.NotEmpty(t, outputs)
	var ref *Output
	for op, out := range outputs {
		require.NotNil(t, out, "op %d produced no output", op)
		if ref == nil {
			ref = out
			continue
		}
		require.Equal(t, ref.Layer, out.Layer, "op %d layer disagrees", op)
		require.True(t, bytes.Equal(ref.Value, out.Value), "op %d value disagrees", op)
		require.True(t, bytes.Equal(ref.Signature, out.Signature),
			"op %d signature disagrees", op)
	}
}

// requireAllAgreeAmongOnline asserts agreement, ignoring offline operators.
func requireAllAgreeAmongOnline(t *testing.T, outputs map[OperatorID]*Output, s *sim) {
	t.Helper()
	online := make(map[OperatorID]*Output)
	for op, out := range outputs {
		if !s.isOffline(op) {
			online[op] = out
		}
	}
	requireAllAgree(t, online)
}
