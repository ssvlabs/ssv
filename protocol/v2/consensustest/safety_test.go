package consensustest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// decidedSingleOpOutcome returns a minimal Outcome with one decided
// honest operator. Sufficient input for ComputeSafetyReport's
// Agreement / Quorum / CommitKind / HostValidity checks (each gated on
// o.Decided + the corresponding CommitAttestation.*Checked bool).
func decidedSingleOpOutcome(att ct.CommitAttestation) ct.Outcome {
	v := []byte("v1")
	return ct.Outcome{
		Decided:      true,
		DecidedValue: v,
		PerOp: map[ct.OperatorID]ct.OperatorOutcome{
			1: {Decided: true, Value: v},
		},
		CommitAttestation: att,
	}
}

// TestSafety_Uninstrumented — zero-value CommitAttestation must leave
// the four new invariants at the default "OK" (no violation reportable).
// Mirrors the graceful-degradation contract every adapter relies on.
func TestSafety_Uninstrumented(t *testing.T) {
	r := ct.ComputeSafetyReport(decidedSingleOpOutcome(ct.CommitAttestation{}))
	require.True(t, r.QuorumBackedDecision, "uninstrumented quorum must default true")
	require.True(t, r.NoEquivocationAccepted, "uninstrumented equivocation must default true")
	require.True(t, r.OBFTCommitKindValid, "uninstrumented commit-kind must default true")
	require.True(t, r.OBFTHostValidityRespect, "uninstrumented host-validity must default true")
	require.False(t, r.IsViolation(), "uninstrumented Outcome must not violate safety")
}

func TestSafety_QuorumBackedDecision(t *testing.T) {
	tests := []struct {
		name      string
		att       ct.CommitAttestation
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "OK_exactly_meets_quorum",
			att:    ct.CommitAttestation{QuorumChecked: true, QuorumSigners: 3, QuorumRequired: 3},
			wantOK: true,
		},
		{
			name:   "OK_above_quorum",
			att:    ct.CommitAttestation{QuorumChecked: true, QuorumSigners: 4, QuorumRequired: 3},
			wantOK: true,
		},
		{
			name:      "VIOLATION_below_quorum",
			att:       ct.CommitAttestation{QuorumChecked: true, QuorumSigners: 2, QuorumRequired: 3},
			wantOK:    false,
			wantPanic: true,
		},
		{
			name:   "OK_no_required_threshold_set",
			att:    ct.CommitAttestation{QuorumChecked: true, QuorumSigners: 0, QuorumRequired: 0},
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := ct.ComputeSafetyReport(decidedSingleOpOutcome(tc.att))
			require.Equal(t, tc.wantOK, r.QuorumBackedDecision)
			require.Equal(t, tc.wantPanic, r.IsViolation())
		})
	}
}

func TestSafety_NoEquivocationAccepted(t *testing.T) {
	tests := []struct {
		name      string
		att       ct.CommitAttestation
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "OK_observed_but_none_accepted",
			att:    ct.CommitAttestation{EquivocationChecked: true, EquivocationsObserved: 5, EquivocationsAccepted: 0},
			wantOK: true,
		},
		{
			name:      "VIOLATION_one_accepted",
			att:       ct.CommitAttestation{EquivocationChecked: true, EquivocationsAccepted: 1},
			wantOK:    false,
			wantPanic: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := ct.ComputeSafetyReport(decidedSingleOpOutcome(tc.att))
			require.Equal(t, tc.wantOK, r.NoEquivocationAccepted)
			require.Equal(t, tc.wantPanic, r.IsViolation())
		})
	}
}

func TestSafety_OBFTCommitKindValid(t *testing.T) {
	tests := []struct {
		name      string
		att       ct.CommitAttestation
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "OK_sigma",
			att:    ct.CommitAttestation{OBFTCommitKindChecked: true, OBFTCommitKind: "sigma"},
			wantOK: true,
		},
		{
			name:   "OK_nr",
			att:    ct.CommitAttestation{OBFTCommitKindChecked: true, OBFTCommitKind: "nr"},
			wantOK: true,
		},
		{
			name:      "VIOLATION_unknown_kind",
			att:       ct.CommitAttestation{OBFTCommitKindChecked: true, OBFTCommitKind: "rogue"},
			wantOK:    false,
			wantPanic: true,
		},
		{
			name:      "VIOLATION_empty_kind",
			att:       ct.CommitAttestation{OBFTCommitKindChecked: true, OBFTCommitKind: ""},
			wantOK:    false,
			wantPanic: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := ct.ComputeSafetyReport(decidedSingleOpOutcome(tc.att))
			require.Equal(t, tc.wantOK, r.OBFTCommitKindValid)
			require.Equal(t, tc.wantPanic, r.IsViolation())
		})
	}
}

func TestSafety_OBFTHostValidityRespect(t *testing.T) {
	tests := []struct {
		name      string
		att       ct.CommitAttestation
		wantOK    bool
		wantPanic bool
	}{
		{
			name:   "OK_no_rejecters",
			att:    ct.CommitAttestation{OBFTHostValidityChecked: true, OBFTHostValidityRejecters: 0},
			wantOK: true,
		},
		{
			name:      "VIOLATION_one_rejecter",
			att:       ct.CommitAttestation{OBFTHostValidityChecked: true, OBFTHostValidityRejecters: 1},
			wantOK:    false,
			wantPanic: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := ct.ComputeSafetyReport(decidedSingleOpOutcome(tc.att))
			require.Equal(t, tc.wantOK, r.OBFTHostValidityRespect)
			require.Equal(t, tc.wantPanic, r.IsViolation())
		})
	}
}

// TestSafety_AttestationGatesOnDecided — the per-decision invariants
// (quorum / commit-kind / host-validity) only run when the cluster
// actually decided. A non-decided sim with adversarial-looking
// attestation values should not trigger a violation.
func TestSafety_AttestationGatesOnDecided(t *testing.T) {
	att := ct.CommitAttestation{
		QuorumChecked:             true,
		QuorumSigners:             0,
		QuorumRequired:            3,
		OBFTCommitKindChecked:     true,
		OBFTCommitKind:            "",
		OBFTHostValidityChecked:   true,
		OBFTHostValidityRejecters: 99,
	}
	r := ct.ComputeSafetyReport(ct.Outcome{
		Decided:           false,
		PerOp:             map[ct.OperatorID]ct.OperatorOutcome{1: {Decided: false, Err: "miss"}},
		CommitAttestation: att,
	})
	require.True(t, r.QuorumBackedDecision)
	require.True(t, r.OBFTCommitKindValid)
	require.True(t, r.OBFTHostValidityRespect)
	require.False(t, r.IsViolation())
}

// TestSafety_EquivocationDoesNotGateOnDecided pins the asymmetric
// counterpart to TestSafety_AttestationGatesOnDecided: equivocation
// MUST fire as a violation regardless of Decided. The reason is
// safety-side: a byzantine that equivocates without the cluster
// deciding is still a fundamental safety violation (the cluster could
// have decided on either of the equivocated values had timing gone
// differently), so the gate is on EquivocationChecked alone — not on
// Decided. This test fails the moment someone adds an `if !decided`
// short-circuit to the equivocation branch in ComputeSafetyReport.
func TestSafety_EquivocationDoesNotGateOnDecided(t *testing.T) {
	att := ct.CommitAttestation{
		EquivocationChecked:   true,
		EquivocationsAccepted: 1,
	}
	// Cluster did NOT decide — but the equivocation evidence is still
	// a safety violation.
	r := ct.ComputeSafetyReport(ct.Outcome{
		Decided:           false,
		PerOp:             map[ct.OperatorID]ct.OperatorOutcome{1: {Decided: false, Err: "miss"}},
		CommitAttestation: att,
	})
	require.False(t, r.NoEquivocationAccepted, "equivocation evidence must fire even when undecided")
	require.True(t, r.IsViolation(), "equivocation violation must be reported regardless of Decided")
}

// hashForTest is a stable hash placeholder for synthetic tests — the
// actual content of the bytes doesn't matter, only that distinct V's
// produce distinct [32]byte values. Using fmt-style prefixed bytes keeps
// the test readable when a violation prints `V_a=68656c...` etc.
func hashForTest(b byte) [32]byte {
	var h [32]byte
	h[0] = b
	return h
}

// TestSafety_HonestCrossPhaseExclusive — bucket-2 B1 invariant.
// Synthetic-outcome tests since no realistic byz pattern can make an
// HONEST operator cross-sign (the check filters byz out by construction
// via Outcome.Byz). The tests hand ComputeSafetyReport a hand-crafted
// Outcome with by-emitter map state simulating a hypothetical EKM
// regression where an honest op emitted σ and NR at the same layer.
//
// Cases cover:
//   - VIOLATION: honest op σ + NR at L_k (the generic case)
//   - VIOLATION (B3 subsumed): honest LEADER σ_V + NR at own layer
//     (the spec-§411 "Each layer's leader's Phase-1 σ counts as their
//     σ-side commitment" rule — surfaces as the same check)
//   - OK: byz emitter cross-signs → filtered out by Outcome.Byz
//   - OK: distinct layers, σ at L_0 and NR at L_1 — legal per spec
//     (cross-phase exclusivity is per-layer, not per-slot)
//   - OK: empty by-emitter maps (adapter didn't instrument, or no
//     observations) → graceful default-true
func TestSafety_HonestCrossPhaseExclusive(t *testing.T) {
	op1 := ct.OperatorID(1)
	op2 := ct.OperatorID(2)
	vA := []byte("V_a")

	tests := []struct {
		name      string
		sigma     []ct.ByEmitterSigmaKey
		nr        []ct.ByEmitterNRKey
		byz       ct.ByzPattern
		wantOK    bool
		wantPanic bool
		wantEvOps []ct.OperatorID // expected operators in CrossPhaseEvidence
	}{
		{
			name:      "OK_empty_maps",
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "OK_distinct_layers_sigma_then_nr",
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)},
			},
			nr: []ct.ByEmitterNRKey{
				{Emitter: op1, Layer: 1}, // different layer → legal
			},
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "VIOLATION_honest_op_sigma_and_nr_same_layer",
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)},
			},
			nr: []ct.ByEmitterNRKey{
				{Emitter: op1, Layer: 0}, // same layer → cross-phase collision
			},
			wantOK:    false,
			wantPanic: true,
			wantEvOps: []ct.OperatorID{op1},
		},
		{
			name: "VIOLATION_leader_sigma_V_and_nr_at_own_layer",
			// B3 case: the layer leader's Phase-1 σ_V is their σ-side
			// commitment; emitting NR at own layer is a spec violation.
			// Same shape as the generic case at the check level.
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xBB)},
			},
			nr: []ct.ByEmitterNRKey{
				{Emitter: op1, Layer: 0},
			},
			wantOK:    false,
			wantPanic: true,
			wantEvOps: []ct.OperatorID{op1},
		},
		{
			name: "OK_byz_emitter_filtered",
			// Byz emitter cross-signs; the check skips ops in Byz set,
			// so this is not a HonestCrossPhaseExclusive violation. (It
			// IS still slashable evidence at the Rule 1 level, recorded
			// in OperatorOutcome.EvidenceByRule — but that's a different
			// invariant.)
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op2, Layer: 0, ValueHash: hashForTest(0xCC)},
			},
			nr: []ct.ByEmitterNRKey{
				{Emitter: op2, Layer: 0},
			},
			byz:       ct.ByzPattern{ByzOperators: []ct.OperatorID{op2}},
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "OK_crashed_op_filtered",
			// Crashed ops are also filtered (the per-op invariants apply
			// only to live honest emitters).
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op2, Layer: 0, ValueHash: hashForTest(0xDD)},
			},
			nr: []ct.ByEmitterNRKey{
				{Emitter: op2, Layer: 0},
			},
			byz:       ct.ByzPattern{Crashed: []ct.OperatorID{op2}},
			wantOK:    true,
			wantPanic: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sigma := make(map[ct.ByEmitterSigmaKey]struct{}, len(tc.sigma))
			for _, k := range tc.sigma {
				sigma[k] = struct{}{}
			}
			nr := make(map[ct.ByEmitterNRKey]struct{}, len(tc.nr))
			for _, k := range tc.nr {
				nr[k] = struct{}{}
			}
			out := ct.Outcome{
				PerOp: map[ct.OperatorID]ct.OperatorOutcome{
					op1: {Decided: true, Value: vA},
				},
				OfflineAgg: ct.OfflineAggReport{
					NoOfflineDoubleV: true,
					SigmaByEmitter:   sigma,
					NRByEmitter:      nr,
				},
				Byz: tc.byz,
			}
			r := ct.ComputeSafetyReport(out)
			require.Equal(t, tc.wantOK, r.HonestCrossPhaseExclusive, "unexpected HonestCrossPhaseExclusive: %s", r)
			require.Equal(t, tc.wantPanic, r.IsViolation())
			if !tc.wantOK {
				require.Len(t, r.CrossPhaseEvidence, len(tc.wantEvOps))
				for i, op := range tc.wantEvOps {
					require.Equal(t, op, r.CrossPhaseEvidence[i].Operator)
				}
			}
		})
	}
}

// TestSafety_HonestSingleSigmaV — bucket-2 B2 invariant. Synthetic
// outcomes simulating an honest op emitting σ on two distinct V's at
// the same layer (a spec-§411 single-σ-V violation, which under correct
// EKM should be cryptographically impossible).
func TestSafety_HonestSingleSigmaV(t *testing.T) {
	op1 := ct.OperatorID(1)
	op2 := ct.OperatorID(2)
	vA := []byte("V_a")

	tests := []struct {
		name      string
		sigma     []ct.ByEmitterSigmaKey
		byz       ct.ByzPattern
		wantOK    bool
		wantPanic bool
		wantEvOps []ct.OperatorID
	}{
		{
			name:      "OK_empty_map",
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "OK_one_V_per_layer",
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)},
				{Emitter: op1, Layer: 1, ValueHash: hashForTest(0xBB)},
			},
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "OK_same_V_different_emitters",
			// Multiple ops emitting σ on the same V at the same layer
			// is the normal σ-quorum buildup pattern; no violation.
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)},
				{Emitter: op2, Layer: 0, ValueHash: hashForTest(0xAA)},
			},
			wantOK:    true,
			wantPanic: false,
		},
		{
			name: "VIOLATION_honest_op_two_Vs_same_layer",
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)},
				{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xBB)}, // same op, same layer, different V
			},
			wantOK:    false,
			wantPanic: true,
			wantEvOps: []ct.OperatorID{op1},
		},
		{
			name: "OK_byz_emitter_filtered",
			sigma: []ct.ByEmitterSigmaKey{
				{Emitter: op2, Layer: 0, ValueHash: hashForTest(0xAA)},
				{Emitter: op2, Layer: 0, ValueHash: hashForTest(0xBB)},
			},
			byz:       ct.ByzPattern{ByzOperators: []ct.OperatorID{op2}},
			wantOK:    true,
			wantPanic: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sigma := make(map[ct.ByEmitterSigmaKey]struct{}, len(tc.sigma))
			for _, k := range tc.sigma {
				sigma[k] = struct{}{}
			}
			out := ct.Outcome{
				PerOp: map[ct.OperatorID]ct.OperatorOutcome{
					op1: {Decided: true, Value: vA},
				},
				OfflineAgg: ct.OfflineAggReport{
					NoOfflineDoubleV: true,
					SigmaByEmitter:   sigma,
				},
				Byz: tc.byz,
			}
			r := ct.ComputeSafetyReport(out)
			require.Equal(t, tc.wantOK, r.HonestSingleSigmaV, "unexpected HonestSingleSigmaV: %s", r)
			require.Equal(t, tc.wantPanic, r.IsViolation())
			if !tc.wantOK {
				require.Len(t, r.SingleSigmaVEvidence, len(tc.wantEvOps))
				for i, op := range tc.wantEvOps {
					require.Equal(t, op, r.SingleSigmaVEvidence[i].Operator)
				}
			}
		})
	}
}

// TestSafety_HonestWalkConsistent — bucket-3 D1 invariant.
// Synthetic-outcome tests: constructs an Outcome with per-op
// ResolveLayerAttempts that simulates a hypothetical Resolve-side
// regression. Two violation cases:
//   - WalkDecidedNoSigmaSource: op decided, trace has no σ-reached
//     layer, AND cluster's local-decide signal has no matching V (no
//     PerOp entry with Decided=true && Round>=0 && Value=oo.Value).
//   - WalkAdvancedPastSigma: op decided locally at layer K, trace has
//     σ-reached at L < K — walk advanced past a σ-decidable layer.
//
// Excluded from violation (OK cases):
//   - Empty / nil ResolveLayerAttempts (adapter not instrumented).
//   - Byz / crashed op (filtered out).
//   - Clip-late ops (oo.Decided=false + Err="missed relay deadline"):
//     the protocol internally decided; the deadline check turned off
//     PerOp.Decided. Not a Resolve regression.
//   - Cert-gossip-decide with no local σ-reached (Round=-1) AND
//     cluster has a local-decider on this V (legitimate catch-up via
//     cert).
//   - Cert-gossip-decide (Round=-1) with a local σ-reached trace
//     (e.g., the op had a parallel partial Resolve that errored after
//     SigmaReached but before completing). Case (b) skips when
//     oo.Round == -1 so this is not flagged.
func TestSafety_HonestWalkConsistent(t *testing.T) {
	op1 := ct.OperatorID(1)
	op2 := ct.OperatorID(2)
	vA := []byte("V_a")
	vB := []byte("V_b")

	tests := []struct {
		name         string
		perOp        map[ct.OperatorID]ct.OperatorOutcome
		byz          ct.ByzPattern
		wantOK       bool
		wantPanic    bool
		wantReasons  []ct.WalkInconsistencyReason
		wantEvOps    []ct.OperatorID
	}{
		{
			name: "OK_no_trace_graceful_default",
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: 0, Value: vA},
				// no ResolveLayerAttempts on op1 — adapter didn't instrument
			},
			wantOK: true,
		},
		{
			name: "OK_decided_at_first_sigma_reached",
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: 0, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "OK_decided_at_fallthrough_layer",
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: 1, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: false, NRReached: true, QV: 3, QEnc: 3},
						{Layer: 1, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "OK_clip_late_decided",
			// Protocol internally decided (trace shows SigmaReached at L_0),
			// but ClipLateDecision turned off oo.Decided because the
			// decision time was past the relay deadline. Not a Resolve
			// regression — should be skipped via the Err=missed-deadline
			// gate.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: false, Round: -1, Value: nil, Err: "missed relay deadline",
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "OK_cert_gossip_decide_with_cluster_local_decider",
			// op1 decided via cert (Round=-1, empty trace). op2 decided
			// locally on the same V (Round=0). op1 is skipped via the
			// empty-trace early gate; op2 passes case (b) (decided at
			// sigmaReachedAt[0]).
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: -1, Value: vA},
				op2: {Decided: true, Round: 0, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "OK_cert_gossip_after_failed_local_resolve_with_cluster_local_decider",
			// op1 ran local Resolve (trace non-empty showing no σ-reached
			// anywhere) and then decided via cert. case (a) cert-gossip
			// branch fires; clusterLocalDecidedOn finds op2 as a real
			// local-decider on V_a → legitimate.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: -1, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: false, QV: 3, SigmaPoolSize: 1, NRReached: true, QEnc: 3},
						{Layer: 1, SigmaReached: false, QV: 3, SigmaPoolSize: 1},
					}},
				op2: {Decided: true, Round: 0, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "VIOLATION_cert_gossip_no_cluster_local_decider",
			// op1 has cert-gossip-decide with a non-empty failed-Resolve
			// trace. No OTHER op is a local-decider on V_a → bogus cert
			// regression. case (a) cert-gossip branch fires
			// clusterLocalDecidedOn(exclude=op1) → no match → flag.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: -1, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: false, QV: 3, SigmaPoolSize: 1},
					}},
				// op2 isn't in PerOp → no cluster-side local-decider on V_a.
			},
			wantOK:      false,
			wantPanic:   true,
			wantReasons: []ct.WalkInconsistencyReason{ct.WalkDecidedNoSigmaSource},
			wantEvOps:   []ct.OperatorID{op1},
		},
		{
			name: "OK_cert_gossip_decide_skip_case_b",
			// op1 has Round=-1 (cert-gossip-decide) but its local trace
			// shows σ-reached at L_0 (an earlier parallel Resolve made
			// progress before cert arrived). Case (b)'s mismatch check
			// must skip when Round=-1.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: -1, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
				op2: {Decided: true, Round: 0, Value: vA, // cluster local-decider on V_a
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK: true,
		},
		{
			name: "VIOLATION_decided_no_sigma_source",
			// op1 claims decided on V_a but its trace shows no
			// σ-reached layer AND no other op locally-decided on V_a.
			// No σ-quorum source anywhere → regression.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: 0, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: false, QV: 3, SigmaPoolSize: 1},
					}},
				// no other op decided on V_a
			},
			wantOK:      false,
			wantPanic:   true,
			wantReasons: []ct.WalkInconsistencyReason{ct.WalkDecidedNoSigmaSource},
			wantEvOps:   []ct.OperatorID{op1},
		},
		{
			name: "VIOLATION_advanced_past_sigma",
			// op1 decided at L_1 but trace shows σ-reached at L_0 too.
			// Walk should have decided at L_0, not advanced to L_1.
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op1: {Decided: true, Round: 1, Value: vA,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
						{Layer: 1, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			wantOK:      false,
			wantPanic:   true,
			wantReasons: []ct.WalkInconsistencyReason{ct.WalkAdvancedPastSigma},
			wantEvOps:   []ct.OperatorID{op1},
		},
		{
			name: "OK_byz_emitter_filtered",
			perOp: map[ct.OperatorID]ct.OperatorOutcome{
				op2: {Decided: true, Round: 1, Value: vB,
					ResolveLayerAttempts: []ct.LayerAttempt{
						{Layer: 0, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
						{Layer: 1, SigmaReached: true, Decided: true, QV: 3, SigmaPoolSize: 3},
					}},
			},
			byz:    ct.ByzPattern{ByzOperators: []ct.OperatorID{op2}},
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ct.Outcome{
				PerOp: tc.perOp,
				Byz:   tc.byz,
			}
			r := ct.ComputeSafetyReport(out)
			require.Equal(t, tc.wantOK, r.HonestWalkConsistent, "unexpected HonestWalkConsistent: %s", r)
			require.Equal(t, tc.wantPanic, r.IsViolation())
			if !tc.wantOK {
				require.Len(t, r.WalkConsistencyEvidence, len(tc.wantReasons))
				for i, reason := range tc.wantReasons {
					require.Equal(t, reason, r.WalkConsistencyEvidence[i].Reason)
					require.Equal(t, tc.wantEvOps[i], r.WalkConsistencyEvidence[i].Operator)
				}
			}
		})
	}
}
