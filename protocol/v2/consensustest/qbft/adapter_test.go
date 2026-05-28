package qbft_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestMeshArrival_NoRefloodToPublisher mirrors the OBFT regression
// test. See its docstring for the design rationale.
func TestMeshArrival_NoRefloodToPublisher(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            4,
		Operators:    ct.MakeOperators(4),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz:          ct.ByzPattern{Kind: ct.ByzNone},
		Seed:         1,
		Delivery:     ct.DeliveryMesh,
		Mesh: ct.MeshConfig{
			HopDelay: ct.LogNormalDelay{Median: btt / 3, Sigma: 0.3},
		},
		TraceEnabled: true,
	}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	ct.AssertNoRefloodToPublisher(t, out.Trace)
}

// TestAdapter_HealthyMesh_N4 — QBFT healthy through the mesh transport.
// See the OBFT adapter's mesh smoke for the rationale.
func TestAdapter_HealthyMesh_N4(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            4,
		Operators:    ct.MakeOperators(4),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz:          ct.ByzPattern{Kind: ct.ByzNone},
		Seed:         1,
		Delivery:     ct.DeliveryMesh,
		Mesh: ct.MeshConfig{
			HopDelay: ct.LogNormalDelay{Median: btt / 3, Sigma: 0.3},
		},
	}
	out, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "mesh-mode healthy should decide at round 1")
	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	t.Logf("mesh-mode healthy: decided at %v on round %d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_HealthyAtClusterSizes verifies the real-instance wrapper runs
// healthy at every SSV-supported cluster size (n=4,7,10,13). Each size uses
// the spec testingutils' tabulated TestKeySet (RSA + BLS shares).
func TestAdapter_HealthyAtClusterSizes(t *testing.T) {
	btt := 200 * time.Millisecond
	for _, n := range ct.ClusterSizes {
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
			out, err := qbftadapter.QBFT0{}.Run(cfg)
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
	out, err := qbftadapter.QBFT0{}.Run(cfg)
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
	out, err := qbftadapter.QBFT0{}.Run(cfg)
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

	out1, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	out2, err := qbftadapter.QBFT0{}.Run(cfg)
	require.NoError(t, err)
	require.Equal(t, out1.Decided, out2.Decided)
	require.Equal(t, out1.DecisionTime, out2.DecisionTime)
	require.Equal(t, out1.DecidedRound, out2.DecidedRound)
	require.Equal(t, len(out1.Trace), len(out2.Trace), "trace length must match")
	for i := range out1.Trace {
		require.Equalf(t, out1.Trace[i], out2.Trace[i], "trace[%d] differs", i)
	}
}

// TestQBFT2R_Healthy_DecidesRound1 — healthy operation decides at round 1
// without engaging the unbounded R2 fallback.
func TestQBFT2R_Healthy_DecidesRound1(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := qbftadapter.QBFT2R{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide at round 1")
	require.Equal(t, 0, out.DecidedRound, "should decide at round 1 (= 0-indexed → 0)")
}

// TestQBFT2R_SilentL0Leader_DecidesRound2 — R1 leader is silent; cluster
// times out at 3·BTT and falls into the unbounded R2, where the next
// leader drives consensus. Confirms R2's "no timer" doesn't prevent it
// from completing normally.
func TestQBFT2R_SilentL0Leader_DecidesRound2(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
	out, err := qbftadapter.QBFT2R{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should decide at round 2 after R1 timeout")
	require.Equal(t, 1, out.DecidedRound, "should decide at round 2 (= 0-indexed → 1)")
	t.Logf("QBFT-2R R2 decision: %v", out.DecisionTime)
}

// TestQBFT2R_TwoSilentLeaders_MISS — both R1 and R2 leaders silent; with
// MaxRounds=2 and R2 unbounded, the cluster sits in R2 until the DES's
// RunUntil cap fires. ClipLateDecision converts the unfinished sim to
// MISS at RelayCutoff. Exercises the round-cap-induced miss path.
func TestQBFT2R_TwoSilentLeaders_MISS(t *testing.T) {
	cfg := qbftNRConfig(7, 200*time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1, 2}}
	out, err := qbftadapter.QBFT2R{}.Run(cfg)
	require.NoError(t, err)
	require.False(t, out.Decided, "no honest leader in first 2 rounds → MISS")
	require.Equal(t, "Cluster never reached consensus before slot end", out.MissReason)
}

// TestQBFT3R_Healthy_DecidesRound1 — healthy decides at round 1.
func TestQBFT3R_Healthy_DecidesRound1(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := qbftadapter.QBFT3R{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide at round 1")
	require.Equal(t, 0, out.DecidedRound, "should decide at round 1 (= 0-indexed → 0)")
}

// TestQBFT3R_TwoSilentLeaders_DecidesRound3 — R1 + R2 leaders silent;
// the cluster reaches R3 (unbounded) where the next honest leader
// drives consensus. Exercises the bounded-R2 → unbounded-R3 transition.
func TestQBFT3R_TwoSilentLeaders_DecidesRound3(t *testing.T) {
	cfg := qbftNRConfig(7, 200*time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1, 2}}
	out, err := qbftadapter.QBFT3R{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "R3 honest leader should drive consensus")
	require.Equal(t, 2, out.DecidedRound, "should decide at round 3 (= 0-indexed → 2)")
	t.Logf("QBFT-3R R3 decision: %v", out.DecisionTime)
}

// TestQBFT3R_ThreeSilentLeaders_MISS — all 3 round leaders silent; with
// MaxRounds=3 and R3 unbounded, the cluster sits in R3 until sim-end
// and clips to MISS at RelayCutoff.
func TestQBFT3R_ThreeSilentLeaders_MISS(t *testing.T) {
	cfg := qbftNRConfig(10, 200*time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1, 2, 3}}
	out, err := qbftadapter.QBFT3R{}.Run(cfg)
	require.NoError(t, err)
	require.False(t, out.Decided, "no honest leader in first 3 rounds → MISS")
	require.Equal(t, "Cluster never reached consensus before slot end", out.MissReason)
}

// TestQBFTNR_DeterministicAcrossRuns confirms the same (cfg, seed)
// produces identical outcomes/traces on repeat runs for both NR variants
// — load-bearing for sweep test stability. Mirrors
// TestAdapter_DeterministicAcrossRuns above.
func TestQBFTNR_DeterministicAcrossRuns(t *testing.T) {
	for _, p := range []ct.Protocol{qbftadapter.QBFT2R{}, qbftadapter.QBFT3R{}} {
		t.Run(p.Name(), func(t *testing.T) {
			cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
			cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}
			cfg.TraceEnabled = true

			out1, err := p.Run(cfg)
			require.NoError(t, err)
			out2, err := p.Run(cfg)
			require.NoError(t, err)
			require.Equal(t, out1.Decided, out2.Decided)
			require.Equal(t, out1.DecisionTime, out2.DecisionTime)
			require.Equal(t, out1.DecidedRound, out2.DecidedRound)
			require.Equal(t, len(out1.Trace), len(out2.Trace), "trace length must match")
			for i := range out1.Trace {
				require.Equalf(t, out1.Trace[i], out2.Trace[i], "trace[%d] differs", i)
			}
		})
	}
}

// qbftNRConfig builds a SimConfig at cluster size n with the same
// defaults DefaultProposerDutyConfig uses for n=4. The NR-family MISS
// tests need n>4 to seat f≥2 silent byz operators, so we can't reuse
// DefaultProposerDutyConfig directly.
func qbftNRConfig(n int, btt time.Duration) ct.SimConfig {
	return ct.SimConfig{
		N:                    n,
		Operators:            ct.MakeOperators(n),
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		Host:                 ct.HostAllValid{},
		Byz:                  ct.ByzPattern{Kind: ct.ByzNone},
		Seed:                 1,
	}
}

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }
