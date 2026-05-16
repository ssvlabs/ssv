package obft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// classifyOBFTMiss is an unreachable target from package obft_test (the
// external test package) and is otherwise only covered via Run-level
// integration tests. These internal-test branches exercise each
// classifier regime in isolation so a future change to one branch
// can't quietly break the others.
//
// Mirrors the three regimes documented above the function:
//   - decided-but-clipped: preDecided=true AND preTime > deadline →
//     "ready at layer N past deadline"
//   - deadlock: deadlockLayer ≥ 0 → "deadlocked at layer X"
//   - exhausted: deadlockLayer == -1 → "never assembled..."
func TestClassifyOBFTMiss_Branches(t *testing.T) {
	const deadline = 3900 * time.Millisecond
	cases := []struct {
		name          string
		preDecided    bool
		preRound      int
		preTime       time.Duration
		deadlockLayer int
		want          string
	}{
		{
			name:          "decided_clipped_layer_0",
			preDecided:    true,
			preRound:      0,
			preTime:       4100 * time.Millisecond,
			deadlockLayer: -1,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
		{
			name:          "decided_clipped_layer_2",
			preDecided:    true,
			preRound:      2,
			preTime:       4500 * time.Millisecond,
			deadlockLayer: -1,
			want:          "Cluster ready to submit at layer 2, past the submit deadline",
		},
		{
			name:          "deadlock_at_layer_0",
			preDecided:    false,
			preRound:      -1,
			preTime:       0,
			deadlockLayer: 0,
			want:          "Cluster deadlocked at layer 0 (σ-pool short, no NR-fallthrough)",
		},
		{
			name:          "deadlock_at_layer_1",
			preDecided:    false,
			preRound:      -1,
			preTime:       0,
			deadlockLayer: 1,
			want:          "Cluster deadlocked at layer 1 (σ-pool short, no NR-fallthrough)",
		},
		{
			name:          "exhausted_no_deadlock",
			preDecided:    false,
			preRound:      -1,
			preTime:       0,
			deadlockLayer: -1,
			want:          "Cluster never assembled a threshold signature at any layer",
		},
		// Decided-and-clipped takes priority over a stray deadlock
		// signal: if any op decided locally past the deadline, that's
		// the cluster-level outcome we report.
		{
			name:          "decided_clipped_outranks_deadlock",
			preDecided:    true,
			preRound:      0,
			preTime:       4500 * time.Millisecond,
			deadlockLayer: 1,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOBFTMiss(tc.preDecided, tc.preRound, tc.preTime, deadline, tc.deadlockLayer)
			require.Equal(t, tc.want, got)
		})
	}
}
