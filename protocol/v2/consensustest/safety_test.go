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
