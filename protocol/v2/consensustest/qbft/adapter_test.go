package qbft_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestAdapter_HealthyAtClusterSizes verifies the real-instance wrapper runs
// healthy at every SSV-supported cluster size (n=4,7,10,13). Each size uses
// the spec testingutils' tabulated TestKeySet (RSA + BLS shares).
func TestAdapter_HealthyAtClusterSizes(t *testing.T) {
	btt := 200 * time.Millisecond
	for _, n := range ct.ClusterSizes {
		n := n
		t.Run(clusterName(n), func(t *testing.T) {
			cfg := ct.SimConfig{
				N:            n,
				Operators:    ct.MakeOperators(n),
				SlotDuration: 12 * time.Second,
				RelayCutoff:  4 * time.Second,
				BTT:          btt,
				Byz:          ct.ByzPattern{Kind: ct.ByzNone},
				Seed:         1,
			}
			out, err := qbftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err, "n=%d Run", n)
			require.True(t, out.Decided, "n=%d should decide healthy", n)
			require.Equal(t, 0, out.DecidedRound, "n=%d should decide at round 1 (= 0-indexed)", n)

			rep := ct.ComputeSafetyReport(out)
			require.True(t, rep.SingleV, "n=%d SingleV: %s", n, rep)
			t.Logf("n=%d: decided at %v on round %d", n, out.DecisionTime, out.DecidedRound)
		})
	}
}

// TestAdapter_RoundChange verifies the round-change path: round-1 leader is
// silent, the cluster timeouts and progresses to round 2 where the next
// leader proposes successfully.
func TestAdapter_RoundChange(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	out, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should decide at round 2 after round 1 timeout")
	require.Equal(t, 1, out.DecidedRound, "should decide at round 2 (= 1-indexed → 1)")
	t.Logf("R2 decision: %v", out.DecisionTime)
}

// TestAdapter_EquivocationFallThrough verifies that byz proposer equivocation
// at round 1 is recovered via round-2 fresh-V proposal from the next honest
// leader.
func TestAdapter_EquivocationFallThrough(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzEquivocateAllNR,
		ByzOperators: []ct.OperatorID{1},
	}
	out, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should decide via round 2 fresh-V")
	require.Equal(t, 1, out.DecidedRound, "should decide at round 2 (= 1-indexed → 1)")
	t.Logf("Equivocation fall-through decision: %v", out.DecisionTime)
}

// TestAdapter_DeterministicAcrossRuns confirms the same (cfg, seed) produces
// identical outcomes on repeat runs — load-bearing for sweep test stability.
func TestAdapter_DeterministicAcrossRuns(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	cfg.TraceEnabled = true

	out1, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	out2, err := qbftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.Equal(t, out1.Decided, out2.Decided)
	require.Equal(t, out1.DecisionTime, out2.DecisionTime)
	require.Equal(t, out1.DecidedRound, out2.DecidedRound)
	require.Equal(t, len(out1.Trace), len(out2.Trace), "trace length must match")
	for i := range out1.Trace {
		require.Equalf(t, out1.Trace[i], out2.Trace[i], "trace[%d] differs", i)
	}
}

func clusterName(n int) string {
	switch n {
	case 4:
		return "n=4"
	case 7:
		return "n=7"
	case 10:
		return "n=10"
	case 13:
		return "n=13"
	default:
		return "n=?"
	}
}
