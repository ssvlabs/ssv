package twoab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// classifyTwoabMiss is unreachable from the external test package and is
// otherwise only covered via Run-level integration tests. These internal
// branches exercise each classifier regime in isolation so a future change
// to one branch can't quietly break the others — in particular the
// deadlock-walk split into the recoverable propagation stall (undelivered)
// vs. the genuine validity / σ-split wedges. Mirrors the regimes documented
// above classifyTwoabMiss.
func TestClassifyTwoabMiss_Branches(t *testing.T) {
	const deadline = 3900 * time.Millisecond
	cases := []struct {
		name          string
		preDecided    bool
		preRound      int
		preTime       time.Duration
		deadlockLayer int
		kind          deadlockKind
		want          string
	}{
		{
			name:          "decided_clipped_layer_0",
			preDecided:    true,
			preRound:      0,
			preTime:       4100 * time.Millisecond,
			deadlockLayer: -1,
			kind:          deadlockNone,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
		{
			name:          "stall_undelivered_layer_0",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 0,
			kind:          deadlockUndelivered,
			want:          "Cluster stalled at layer 0 — value didn't reach σ-quorum in time (undelivered)",
		},
		{
			name:          "deadlock_validity_layer_0",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 0,
			kind:          deadlockValidity,
			want:          "Cluster deadlocked at layer 0 (validity split — σ impossible for the dissenting cohort, NR-default gated)",
		},
		{
			name:          "deadlock_split_layer_1",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 1,
			kind:          deadlockSplit,
			want:          "Cluster deadlocked at layer 1 (σ split across values, none reaching qV; NR-default gated)",
		},
		{
			name:          "exhausted_no_deadlock",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: -1,
			kind:          deadlockNone,
			want:          "Cluster never assembled a threshold signature at any layer",
		},
		// Decided-and-clipped outranks a stray deadlock signal: if any op
		// decided locally past the deadline, that's the cluster outcome.
		{
			name:          "decided_clipped_outranks_deadlock",
			preDecided:    true,
			preRound:      0,
			preTime:       4500 * time.Millisecond,
			deadlockLayer: 0,
			kind:          deadlockUndelivered,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyTwoabMiss(tc.preDecided, tc.preRound, tc.preTime, deadline, tc.deadlockLayer, tc.kind)
			require.Equal(t, tc.want, got)
		})
	}
}
